package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
)

// VideoRequest is the input contract used by the Generate handler.
type VideoRequest struct {
	Prompt      string
	ModelSlug   string // optional, defaults to DefaultVideoModel
	AspectRatio string // optional, e.g. "16:9", "9:16"
	Resolution  string // friendly alias: 480p / 720p / 1080p (or RESOLUTION_*)
	Duration    int    // seconds, 0 = use model default
	Audio       bool   // motion_has_audio
	ImageURL    string // optional, used as start_frame for image-to-video
	ImageID     string // optional, pre-uploaded init image id (skip URL upload)
	ImageBytes  []byte // optional local reference; uploaded by the same account that submits the video
	ImageExt    string
	// ClientRequestID is an idempotency key supplied by VideoClaw/Infinite Canvas.
	// It is persisted as metadata so a retry can reuse a completed result.
	ClientRequestID string
}

// VideoResponse mirrors the OpenAI-compatible image response.
type VideoResponse struct {
	Created  int64                 `json:"created"`
	Data     []VideoResponseItem   `json:"data"`
	Provider VideoResponseProvider `json:"provider"`
}

// VideoResponseItem is one playable video entry. We expose mp4_url as the
// canonical field plus url for clients that consume the OpenAI image schema.
type VideoResponseItem struct {
	URL          string `json:"url"`
	MP4URL       string `json:"mp4_url"`
	GIFURL       string `json:"gif_url,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

// VideoResponseProvider carries diagnostic metadata about which cookie/model handled the job.
type VideoResponseProvider struct {
	GenerationID     string   `json:"generation_id"`
	CreditCost       int64    `json:"credit_cost"`
	CreditCostSource string   `json:"credit_cost_source"`
	UsedCookieID     int64    `json:"used_cookie_id"`
	Model            string   `json:"model"`
	Resolution       string   `json:"resolution"`
	Duration         int      `json:"duration"`
	AspectRatio      string   `json:"aspect_ratio"`
	Audio            bool     `json:"audio"`
	SavedFiles       []string `json:"saved_files"`
	AutoSaveEnabled  bool     `json:"auto_save_enabled"`
	SaveError        string   `json:"save_error,omitempty"`
	ClientRequestID  string   `json:"client_request_id,omitempty"`
}

// videoTimeout/videoPoll govern how long the local background task follows a
// Leonardo generation. Some video models can remain queued or rendering well
// beyond the old eight-minute limit, even though Leonardo is still processing
// them. Keep a generous safety ceiling; browser clients poll the same task
// until this worker reports a real terminal state.
const (
	videoTimeout = 2 * time.Hour
	videoPollInt = 4 * time.Second
)

// GenerateVideo runs the full Seedance/video pipeline against the cookie pool.
// The cookie rotation, auth-error handling, and auto-disable behaviour mirror
// LeonardoPool.Generate so operators get one consistent failure model.
func (p *LeonardoPool) GenerateVideo(req VideoRequest) (*VideoResponse, error) {
	model := LookupVideoModel(req.ModelSlug)
	if req.ModelSlug != "" && model == nil {
		return nil, newPublicError(400, fmt.Sprintf("不支持的视频模型：%s", req.ModelSlug))
	}
	if model == nil {
		model = DefaultVideoModel()
	}
	if model == nil {
		return nil, newPublicError(500, "尚未配置视频模型")
	}

	mode := model.ResolveResolution(req.Resolution)
	aspect := strings.TrimSpace(req.AspectRatio)
	if aspect == "" {
		aspect = model.DefaultAspect
	}
	width, height := model.ResolveDimensions(mode, aspect)
	if (width == 0 || height == 0) && mode != "RESOLUTION_AUTO" {
		return nil, newPublicError(400, fmt.Sprintf(
			"unsupported aspect_ratio %q for %s at %s",
			aspect, model.Slug, mode,
		))
	}

	duration := model.ClampDuration(req.Duration)

	if !model.SupportsAudio() && req.Audio {
		return nil, newPublicError(400, fmt.Sprintf("模型 %s 不支持音频", model.Slug))
	}
	if !model.SupportsRefImage && (strings.TrimSpace(req.ImageURL) != "" || strings.TrimSpace(req.ImageID) != "" || len(req.ImageBytes) > 0) {
		return nil, newPublicError(400, fmt.Sprintf("模型 %s 不支持参考图片", model.Slug))
	}
	if model.RequiresRefImage && strings.TrimSpace(req.ImageURL) == "" && strings.TrimSpace(req.ImageID) == "" && len(req.ImageBytes) == 0 {
		return nil, newPublicError(400, fmt.Sprintf("模型 %s 必须提供首帧图片", model.Slug))
	}
	audio := req.Audio
	if model.AudioPolicy == AudioRequired {
		audio = true
	}
	if model.AudioPolicy == AudioNone {
		audio = false
	}

	cookies, err := p.store.ListActiveCookies()
	if err != nil {
		return nil, fmt.Errorf("service: list cookies: %w", err)
	}
	if len(cookies) == 0 {
		return nil, newPublicError(400, "账号池中没有可用账号")
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, newPublicError(400, "prompt 不能为空")
	}

	var errs []string

	for _, cookie := range cookies {
		if p.shouldSkipCookieNow(cookie) {
			errs = append(errs, fmt.Sprintf("cookie#%d: cooldown (auth recently failed)", cookie.ID))
			continue
		}

		for attempt := 0; attempt < 2; attempt++ {
			resolvedSession := p.resolveTokenForCookieAttempt(cookie, attempt > 0)
			token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
			if token == "" {
				if accountChanged {
					errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, accountChangedMessage))
					break
				}
				if resolvedSession.Err != nil {
					errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, resolvedSession.Err.Error()))
				} else {
					errs = append(errs, fmt.Sprintf("cookie#%d: 登录会话暂时不可用", cookie.ID))
				}
				break
			}

			info, err := p.api.GetUserInfo(token)
			if err != nil {
				if isAuthError(err.Error()) && attempt == 0 {
					p.reportRejectedJWT(cookie, err.Error())
					continue
				}
				if isAuthError(err.Error()) {
					p.reportRejectedJWT(cookie, err.Error())
				}
				errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, err.Error()))
				break
			}
			if err := p.ensureCookieIdentity(cookie, info); err != nil {
				errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, err.Error()))
				break
			}

			p.refreshFallbackToken(cookie.ID, cookie.Value, token)
			_ = p.store.UpdateCookieProfile(cookie.ID, info.ID, info.Email, info.Tokens)

			if info.Tokens <= 0 {
				p.disableCookie(cookie.ID, "DEPLETED", "Token balance is empty")
				errs = append(errs, fmt.Sprintf("cookie#%d: depleted (auto-disabled)", cookie.ID))
				break
			}

			// Image-to-video: prefer pre-uploaded id, else upload from URL.
			var startFrame *leonardo.VideoGuidanceImage
			if len(req.ImageBytes) > 0 {
				initID, err := p.api.UploadImageBytes(token, req.ImageBytes, req.ImageExt)
				if err != nil {
					if isAuthError(err.Error()) && attempt == 0 {
						p.reportRejectedJWT(cookie, err.Error())
						continue
					}
					if isAuthError(err.Error()) {
						p.reportRejectedJWT(cookie, err.Error())
					}
					errs = append(errs, fmt.Sprintf("账号#%d 上传首帧失败：%s", cookie.ID, err.Error()))
					break
				}
				startFrame = &leonardo.VideoGuidanceImage{ID: initID, Type: "UPLOADED"}
			} else if id := strings.TrimSpace(req.ImageID); id != "" {
				startFrame = &leonardo.VideoGuidanceImage{ID: id, Type: "UPLOADED"}
			} else if u := strings.TrimSpace(req.ImageURL); u != "" {
				initID, err := p.api.UploadImageURL(token, u)
				if err != nil {
					if isAuthError(err.Error()) && attempt == 0 {
						p.reportRejectedJWT(cookie, err.Error())
						continue
					}
					if isAuthError(err.Error()) {
						p.reportRejectedJWT(cookie, err.Error())
					}
					errs = append(errs, fmt.Sprintf("账号#%d 上传首帧失败：%s", cookie.ID, err.Error()))
					break
				}
				startFrame = &leonardo.VideoGuidanceImage{ID: initID, Type: "UPLOADED"}
			}

			input := leonardo.VideoInput{
				ModelSlug:        model.LeonardoModelValue(),
				Prompt:           prompt,
				Width:            width,
				Height:           height,
				DurationSec:      duration,
				Mode:             mode,
				HasAudio:         audio,
				IncludeMode:      model.IncludeMode,
				IncludeAudio:     model.SupportsAudio(),
				IncludeSeed:      model.IncludeSeed,
				AudioParameter:   model.AudioParameter,
				DirectStartFrame: model.DirectStartFrame,
				Seed:             -1,
				Quantity:         1,
				StartFrame:       startFrame,
				Public:           true,
			}

			submission, err := p.api.CreateVideoGeneration(token, input)
			if err != nil {
				if isAuthError(err.Error()) && attempt == 0 {
					p.reportRejectedJWT(cookie, err.Error())
					continue
				}
				if isAuthError(err.Error()) {
					p.reportRejectedJWT(cookie, err.Error())
				}
				errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, err.Error()))
				break
			}

			genID := submission.GenerationID
			completion := p.api.WaitForVideoCompletion(token, genID, videoTimeout, videoPollInt)
			if !completion.Success {
				if isAuthError(completion.Error) && attempt == 0 {
					p.reportRejectedJWT(cookie, completion.Error)
					continue
				}
				if isAuthError(completion.Error) {
					p.reportRejectedJWT(cookie, completion.Error)
				}
				errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, completion.Error))
				break
			}

			_ = p.store.MarkCookieUsed(cookie.ID)
			// Re-sync balance after spending credits so the UI updates.
			postBalance := p.refreshBalanceAfterUse(cookie.ID, token)
			creditCost := submission.CreditCost
			creditCostSource := "api_credit_cost"
			if creditCost <= 0 && postBalance >= 0 && info.Tokens > postBalance {
				creditCost = info.Tokens - postBalance
				creditCostSource = "balance_delta"
			} else if creditCost <= 0 {
				creditCostSource = "unavailable"
			}

			// Auto-save mp4 + thumbnail when enabled. Same setting key as image
			// save flow so operators only configure one directory.
			autoSaveEnabled := false
			if v, _ := p.store.GetSetting("auto_save_images", "0"); v == "1" {
				autoSaveEnabled = true
			}

			items := make([]VideoResponseItem, 0, len(completion.Media))
			mp4URLs := make([]string, 0, len(completion.Media))
			thumbnails := make([]string, 0, len(completion.Media))
			for _, m := range completion.Media {
				items = append(items, VideoResponseItem{
					URL:          m.MP4URL, // OpenAI-style schema: `url` is the primary asset
					MP4URL:       m.MP4URL,
					GIFURL:       m.GIFURL,
					ThumbnailURL: m.ThumbnailURL,
					Width:        m.Width,
					Height:       m.Height,
				})
				mp4URLs = append(mp4URLs, m.MP4URL)
				if m.ThumbnailURL != "" {
					thumbnails = append(thumbnails, m.ThumbnailURL)
				}
			}

			var savedFiles []string
			var saveErrMsg string
			if autoSaveEnabled && len(mp4URLs) > 0 {
				combined := append([]string{}, mp4URLs...)
				combined = append(combined, thumbnails...)
				files, err := p.saveGeneratedImages(genID, combined)
				savedFiles = files
				if err != nil {
					saveErrMsg = err.Error()
				}
			}

			_ = p.store.AddProviderGenerationLog(
				"leonardo", genID, info.ID, "video",
				generationMetadata(req.ClientRequestID), cookie.ID,
				model.Slug, aspect, prompt, mp4URLs, savedFiles,
				autoSaveEnabled, "success", "",
			)

			return &VideoResponse{
				Created: time.Now().Unix(),
				Data:    items,
				Provider: VideoResponseProvider{
					GenerationID:     genID,
					CreditCost:       creditCost,
					CreditCostSource: creditCostSource,
					UsedCookieID:     cookie.ID,
					Model:            model.Slug,
					Resolution:       mode,
					Duration:         duration,
					AspectRatio:      aspect,
					Audio:            audio,
					SavedFiles:       orEmpty(savedFiles),
					AutoSaveEnabled:  autoSaveEnabled,
					SaveError:        saveErrMsg,
					ClientRequestID:  req.ClientRequestID,
				},
			}, nil
		}
	}

	detail := "所有 Leonardo 账号均无法完成视频生成。"
	if len(errs) > 0 {
		if len(errs) > 6 {
			errs = errs[:6]
		}
		detail = "所有 Leonardo 账号均无法完成视频生成：" + strings.Join(errs, "；")
	}
	return nil, newPublicError(503, detail)
}

// silence unused import warning in case we trim further later.
var _ = errors.New
