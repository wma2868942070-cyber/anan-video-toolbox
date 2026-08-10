package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type adobeVideoJob struct {
	ID                string
	CreatedAt         int64
	UpdatedAt         int64
	Status            string
	Model             string
	Prompt            string
	AspectRatio       string
	ResultURL         string
	Error             string
	ProviderAccountID string
	ProviderMetadata  map[string]any
	ClientRequestID   string
}

func (s *Server) adobeVideoTaskEnvelope(job *adobeVideoJob) gin.H {
	out := gin.H{
		"id": job.ID, "object": "video", "status": job.Status, "model": job.Model,
		"created_at": job.CreatedAt, "updated_at": job.UpdatedAt,
		"status_url":  "/adobe/v1/videos/" + job.ID,
		"content_url": "/adobe/v1/videos/" + job.ID + "/content",
	}
	if job.Error != "" {
		out["error"] = gin.H{"message": job.Error}
	}
	if job.ResultURL != "" {
		out["video_url"] = job.ResultURL
		out["provider"] = job.ProviderMetadata
	}
	return out
}

var adobeVideoURLPattern = regexp.MustCompile(`(?i)<video[^>]+src=['\"]([^'\"]+)['\"]`)
var adobeImageURLPattern = regexp.MustCompile(`(?i)!\[[^\]]*\]\(([^)]+)\)`)

func (s *Server) handleAdobeHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	payload, err := s.adobe.Health(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "offline", "running": false, "error": adobeGatewayError(err),
		})
		return
	}
	payload["running"] = true
	payload["status"] = "ok"
	c.JSON(http.StatusOK, payload)
}

func (s *Server) handleAdobeModels(c *gin.Context) {
	variants, err := s.loadAdobeModelVariants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "Adobe 模型读取失败："+adobeGatewayError(err))
		return
	}
	if strings.EqualFold(strings.TrimSpace(c.Query("variants")), "1") || strings.EqualFold(strings.TrimSpace(c.Query("variants")), "true") {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": buildAdobeVariantModels(variants)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": buildAdobeCanonicalModels(variants)})
}

func adobeModelMetadata(id, description string) map[string]any {
	lower := strings.ToLower(id)
	mediaType := "video"
	if strings.Contains(lower, "nano-banana") || strings.Contains(lower, "gpt-image") {
		mediaType = "image"
	}
	family := "Adobe Firefly"
	switch {
	case strings.Contains(lower, "nano-banana-pro"):
		family = "Nano Banana Pro"
	case strings.Contains(lower, "nano-banana2"):
		family = "Nano Banana 2"
	case strings.Contains(lower, "nano-banana"):
		family = "Nano Banana"
	case strings.Contains(lower, "gpt-image"):
		family = "GPT Image"
	case strings.Contains(lower, "sora2-pro"):
		family = "Sora 2 Pro"
	case strings.Contains(lower, "sora2"):
		family = "Sora 2"
	case strings.Contains(lower, "gemini-omni"):
		family = "Gemini Omni"
	case strings.Contains(lower, "veo31-fast"):
		family = "Veo 3.1 Fast"
	case strings.Contains(lower, "veo31-ref"):
		family = "Veo 3.1 Ref"
	case strings.Contains(lower, "veo31"):
		family = "Veo 3.1"
	case strings.Contains(lower, "seedance20-fast"):
		family = "Seedance 2.0 Fast"
	case strings.Contains(lower, "seedance20"):
		family = "Seedance 2.0"
	case strings.Contains(lower, "kling-o3"):
		family = "Kling O3"
	case strings.Contains(lower, "kling3"):
		family = "Kling 3.0"
	}
	duration := 0
	if match := regexp.MustCompile(`-(\d+)s(?:-|$)`).FindStringSubmatch(lower); len(match) == 2 {
		duration, _ = strconv.Atoi(match[1])
	}
	resolution := ""
	if match := regexp.MustCompile(`-(480p|720p|1080p|1k|2k|4k)(?:-|$)`).FindStringSubmatch(lower); len(match) == 2 {
		resolution = match[1]
	}
	ratio := ""
	if match := regexp.MustCompile(`-(\d+)x(\d+)(?:-|$)`).FindStringSubmatch(lower); len(match) == 3 {
		ratio = normalizeAdobeAspect(match[1] + ":" + match[2])
	}
	// Conservative fallback for older sidecars that only return id/description.
	// Capabilities are not synonymous with media type: Gemini Omni has video
	// references but no native-audio flag, while Sora is currently unavailable.
	supportsAudio := mediaType == "video" &&
		(strings.Contains(lower, "veo31") || strings.Contains(lower, "kling") || strings.Contains(lower, "seedance20"))
	supportsImageReference := mediaType == "image" ||
		(mediaType == "video" &&
			!strings.Contains(lower, "sora2") &&
			(strings.Contains(lower, "gemini-omni") || strings.Contains(lower, "veo31") || strings.Contains(lower, "kling") || strings.Contains(lower, "seedance20")))
	supportsStartFrame := mediaType == "video" && supportsImageReference
	if strings.Contains(lower, "veo31-ref") || strings.Contains(lower, "gemini-omni") {
		supportsStartFrame = false
	}
	supportsVideoReference := mediaType == "video" && (strings.Contains(lower, "gemini-omni") || strings.Contains(lower, "seedance20"))
	supportsAudioReference := mediaType == "video" && strings.Contains(lower, "seedance20")
	displayName := adobeChineseModelName(family, mediaType, duration, resolution, ratio, supportsAudio)
	if strings.TrimSpace(displayName) == "" {
		displayName = firstNonEmpty(description, id)
	}
	return map[string]any{
		"id": id, "object": "model", "owned_by": "adobe2api", "provider": "adobe",
		"name": displayName, "display_name": displayName, "family": family, "type": mediaType,
		"duration": duration, "resolution": resolution, "aspect_ratio": ratio,
		"supports_audio":           supportsAudio,
		"supports_image_reference": supportsImageReference,
		"supports_start_frame":     supportsStartFrame,
		"supports_video_reference": supportsVideoReference,
		"supports_audio_reference": supportsAudioReference,
	}
}

func adobeChineseModelName(family, mediaType string, duration int, resolution, ratio string, supportsAudio bool) string {
	kind := "视频"
	if mediaType == "image" {
		kind = "图片"
	}
	parts := []string{strings.TrimSpace(family), kind}
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("%d秒", duration))
	}
	if resolution != "" {
		if strings.HasSuffix(resolution, "k") {
			resolution = strings.ToUpper(resolution)
		}
		parts = append(parts, resolution)
	}
	if ratio != "" {
		parts = append(parts, ratio)
	}
	if mediaType == "video" && supportsAudio {
		parts = append(parts, "有声")
	}
	return strings.Join(parts, " · ")
}

func (s *Server) handleAdobeImageGeneration(c *gin.Context) {
	var request map[string]any
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "JSON 格式无效："+err.Error())
		return
	}
	prompt := strings.TrimSpace(fmt.Sprint(request["prompt"]))
	if prompt == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	requestedModel := strings.TrimSpace(fmt.Sprint(request["model"]))
	if requestedModel == "" || requestedModel == "<nil>" {
		writeError(c, http.StatusBadRequest, "model 不能为空")
		return
	}
	variants, err := s.loadAdobeModelVariants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "Adobe 模型读取失败："+adobeGatewayError(err))
		return
	}
	resolvedModel, err := selectAdobeModelVariant(variants, requestedModel, "image", request)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	requestID := strings.TrimSpace(fmt.Sprint(request["client_request_id"]))
	if requestID == "<nil>" {
		requestID = ""
	}
	if s.tryReuseSuccessfulImage(c, "adobe", requestedModel, prompt, requestID) {
		return
	}
	request["model"] = resolvedModel
	payload, err := s.adobe.ServiceJSON(c.Request.Context(), http.MethodPost, "/v1/images/generations", request)
	if err != nil {
		message := adobeGatewayError(err)
		_ = s.store.AddProviderGenerationLog(
			"adobe", newVideoJobID(), "", "image", "{}", 0,
			requestedModel, adobeAspect(request), prompt,
			nil, nil, false, "failed", message,
		)
		writeError(c, http.StatusBadGateway, message)
		return
	}
	rewriteAdobeURLs(payload, c.Request.Host)
	urls := adobeImageURLs(payload)
	providerID := adobeProviderGenerationID(payload, urls)
	provider, accountID := adobeProviderInfo(payload)
	metadata := generationMetadataWithProvider(provider, requestID)
	_ = s.store.AddProviderGenerationLog(
		"adobe", providerID, accountID, "image", metadata, 0,
		requestedModel, adobeAspect(request), prompt,
		urls, nil, false, "success", "",
	)
	c.JSON(http.StatusOK, payload)
}

func (s *Server) handleAdobeImageEdit(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(128 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "图片编辑表单无效："+err.Error())
		return
	}
	prompt := strings.TrimSpace(c.PostForm("prompt"))
	model := strings.TrimSpace(c.PostForm("model"))
	requestID := strings.TrimSpace(c.PostForm("client_request_id"))
	if prompt == "" || model == "" {
		writeError(c, http.StatusBadRequest, "prompt 和 model 不能为空")
		return
	}
	selector := map[string]any{
		"model": model, "size": c.PostForm("size"), "quality": c.PostForm("quality"),
		"aspect_ratio": firstNonEmpty(c.PostForm("aspect_ratio"), c.PostForm("ratio")),
	}
	variants, err := s.loadAdobeModelVariants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "Adobe 模型读取失败："+adobeGatewayError(err))
		return
	}
	resolvedModel, err := selectAdobeModelVariant(variants, model, "image", selector)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if s.tryReuseSuccessfulImage(c, "adobe", model, prompt, requestID) {
		return
	}
	content := []any{map[string]any{"type": "text", "text": prompt}}
	if c.Request.MultipartForm != nil {
		for _, header := range c.Request.MultipartForm.File["image"] {
			item, err := adobeContentFromFile("image", header)
			if err != nil {
				writeError(c, http.StatusBadRequest, err.Error())
				return
			}
			content = append(content, item)
		}
	}
	request := map[string]any{
		"model":    resolvedModel,
		"prompt":   prompt,
		"messages": []any{map[string]any{"role": "user", "content": content}},
	}
	payload, err := s.adobe.ServiceJSON(c.Request.Context(), http.MethodPost, "/v1/chat/completions", request)
	if err != nil {
		message := adobeGatewayError(err)
		_ = s.store.AddProviderGenerationLog("adobe", newVideoJobID(), "", "image", "{}", 0, model, c.PostForm("size"), prompt, nil, nil, false, "failed", message)
		writeError(c, http.StatusBadGateway, message)
		return
	}
	rewriteAdobeURLs(payload, c.Request.Host)
	imageURL := adobeChatImageURL(payload)
	if imageURL == "" {
		writeError(c, http.StatusBadGateway, "Adobe2API 未返回编辑后的图片")
		return
	}
	provider, accountID := adobeProviderInfo(payload)
	metadata := generationMetadataWithProvider(provider, requestID)
	providerID := filepath.Base(strings.SplitN(imageURL, "?", 2)[0])
	_ = s.store.AddProviderGenerationLog("adobe", providerID, accountID, "image", metadata, 0, model, c.PostForm("size"), prompt, []string{imageURL}, nil, false, "success", "")
	c.JSON(http.StatusOK, gin.H{
		"created": time.Now().Unix(), "model": model,
		"data": []gin.H{{"url": imageURL}}, "provider": provider,
	})
}

func (s *Server) handleAdobeCreateVideoTask(c *gin.Context) {
	request, err := bindAdobeVideoRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	prompt := strings.TrimSpace(fmt.Sprint(request["prompt"]))
	if prompt == "" {
		prompt = promptFromAdobeMessages(request["messages"])
	}
	if prompt == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	model := strings.TrimSpace(fmt.Sprint(request["model"]))
	if model == "" {
		writeError(c, http.StatusBadRequest, "model 不能为空")
		return
	}
	variants, err := s.loadAdobeModelVariants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "Adobe 模型读取失败："+adobeGatewayError(err))
		return
	}
	resolvedModel, err := selectAdobeModelVariant(variants, model, "video", request)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	request["model"] = resolvedModel
	requestID := strings.TrimSpace(fmt.Sprint(request["client_request_id"]))
	if requestID == "<nil>" {
		requestID = ""
	}
	if requestID != "" {
		s.adobeMu.RLock()
		existingID := s.adobeRequestJobs[requestID]
		existing := s.adobeJobs[existingID]
		if existing != nil {
			copy := *existing
			s.adobeMu.RUnlock()
			c.JSON(http.StatusAccepted, s.adobeVideoTaskEnvelope(&copy))
			return
		}
		s.adobeMu.RUnlock()
	}
	if reused, ok := s.reusedAdobeVideoJob(model, prompt, requestID); ok {
		s.adobeMu.Lock()
		s.adobeJobs[reused.ID] = reused
		if requestID != "" {
			s.adobeRequestJobs[requestID] = reused.ID
		}
		s.adobeMu.Unlock()
		c.JSON(http.StatusAccepted, s.adobeVideoTaskEnvelope(reused))
		return
	}
	if _, exists := request["messages"]; !exists {
		content, _ := request["content"].([]any)
		if len(content) == 0 {
			content = []any{map[string]any{"type": "text", "text": prompt}}
		}
		request["messages"] = []any{map[string]any{"role": "user", "content": content}}
	}
	if _, exists := request["generate_audio"]; !exists {
		request["generate_audio"] = true
	}
	delete(request, "prompt")
	job := &adobeVideoJob{
		ID: newVideoJobID(), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		Status: "queued", Model: model, Prompt: prompt, AspectRatio: adobeAspect(request),
		ClientRequestID: requestID,
	}
	s.adobeMu.Lock()
	s.adobeJobs[job.ID] = job
	if requestID != "" {
		s.adobeRequestJobs[requestID] = job.ID
	}
	if len(s.adobeJobs) > 200 {
		for id, old := range s.adobeJobs {
			if old.Status == "completed" || old.Status == "failed" {
				delete(s.adobeJobs, id)
				break
			}
		}
	}
	s.adobeMu.Unlock()

	go s.runAdobeVideoJob(job, request)
	c.JSON(http.StatusAccepted, s.adobeVideoTaskEnvelope(job))
}

func (s *Server) runAdobeVideoJob(job *adobeVideoJob, request map[string]any) {
	s.adobeMu.Lock()
	job.Status = "in_progress"
	job.UpdatedAt = time.Now().Unix()
	s.adobeMu.Unlock()
	payload, err := s.adobe.ServiceJSON(context.Background(), http.MethodPost, "/v1/chat/completions", request)
	if err != nil {
		message := adobeGatewayError(err)
		s.adobeMu.Lock()
		job.Status = "failed"
		job.Error = message
		job.UpdatedAt = time.Now().Unix()
		if job.ClientRequestID != "" {
			delete(s.adobeRequestJobs, job.ClientRequestID)
		}
		s.adobeMu.Unlock()
		_ = s.store.AddProviderGenerationLog("adobe", job.ID, "", "video", "{}", 0, job.Model, job.AspectRatio, job.Prompt, nil, nil, false, "failed", message)
		return
	}
	rewriteAdobeURLs(payload, "127.0.0.1:"+s.cfg.Port)
	resultURL := adobeChatVideoURL(payload)
	provider, accountID := adobeProviderInfo(payload)
	metadata := generationMetadataWithProvider(provider, job.ClientRequestID)
	s.adobeMu.Lock()
	job.UpdatedAt = time.Now().Unix()
	job.ProviderMetadata = provider
	job.ProviderAccountID = accountID
	if resultURL == "" {
		job.Status = "failed"
		job.Error = "Adobe2API 未返回可播放视频地址"
	} else {
		job.Status = "completed"
		job.ResultURL = resultURL
	}
	s.adobeMu.Unlock()
	status := "success"
	errorText := ""
	urls := []string{resultURL}
	if resultURL == "" {
		status = "failed"
		errorText = job.Error
		urls = nil
	}
	_ = s.store.AddProviderGenerationLog("adobe", job.ID, accountID, "video", metadata, 0, job.Model, job.AspectRatio, job.Prompt, urls, nil, false, status, errorText)
}

func (s *Server) handleAdobeGetVideoTask(c *gin.Context) {
	s.adobeMu.RLock()
	job, ok := s.adobeJobs[c.Param("id")]
	if ok {
		copy := *job
		job = &copy
	}
	s.adobeMu.RUnlock()
	if !ok {
		writeError(c, http.StatusNotFound, "Adobe 视频任务不存在")
		return
	}
	out := gin.H{
		"id": job.ID, "object": "video", "status": job.Status, "model": job.Model,
		"created_at": job.CreatedAt, "updated_at": job.UpdatedAt,
	}
	if job.Error != "" {
		out["error"] = gin.H{"message": job.Error}
	}
	if job.ResultURL != "" {
		out["video_url"] = job.ResultURL
		out["content_url"] = "/adobe/v1/videos/" + job.ID + "/content"
		out["provider"] = job.ProviderMetadata
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleAdobeGetVideoContent(c *gin.Context) {
	s.adobeMu.RLock()
	job, ok := s.adobeJobs[c.Param("id")]
	resultURL := ""
	if ok {
		resultURL = job.ResultURL
	}
	s.adobeMu.RUnlock()
	if !ok {
		writeError(c, http.StatusNotFound, "Adobe 视频任务不存在")
		return
	}
	if resultURL == "" {
		writeError(c, http.StatusConflict, "Adobe 视频尚未完成")
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, resultURL)
}

func (s *Server) handleAdobeGenerated(c *gin.Context) {
	filename := filepath.Base(strings.TrimSpace(c.Param("filename")))
	if filename == "" || filename == "." || filename != c.Param("filename") {
		c.Status(http.StatusNotFound)
		return
	}
	resp, err := s.adobe.ServiceRaw(c.Request.Context(), http.MethodGet, "/generated/"+url.PathEscape(filename), "", nil)
	if err != nil {
		writeError(c, http.StatusBadGateway, adobeGatewayError(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.Status(resp.StatusCode)
		return
	}
	if value := resp.Header.Get("Content-Type"); value != "" {
		c.Header("Content-Type", value)
	}
	if value := resp.Header.Get("Content-Length"); value != "" {
		c.Header("Content-Length", value)
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func bindAdobeVideoRequest(c *gin.Context) (map[string]any, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if !strings.HasPrefix(contentType, "multipart/form-data") && !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		var payload map[string]any
		if err := c.ShouldBindJSON(&payload); err != nil {
			return nil, fmt.Errorf("JSON 格式无效：%w", err)
		}
		return payload, nil
	}
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(256 << 20); err != nil {
			return nil, fmt.Errorf("表单格式无效：%w", err)
		}
	} else if err := c.Request.ParseForm(); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model": c.PostForm("model"), "prompt": c.PostForm("prompt"),
		"aspect_ratio": firstNonEmpty(c.PostForm("aspect_ratio"), c.PostForm("ratio")),
		"resolution":   firstNonEmpty(c.PostForm("resolution"), c.PostForm("resolution_name")),
		"duration":     firstNonEmpty(c.PostForm("duration"), c.PostForm("seconds")),
	}
	if requestID := strings.TrimSpace(c.PostForm("client_request_id")); requestID != "" {
		payload["client_request_id"] = requestID
	}
	if raw := firstNonEmpty(c.PostForm("generate_audio"), c.PostForm("audio")); raw != "" {
		payload["generate_audio"] = raw == "1" || strings.EqualFold(raw, "true")
	}
	content := []any{map[string]any{"type": "text", "text": c.PostForm("prompt")}}
	if c.Request.MultipartForm != nil {
		for key, headers := range c.Request.MultipartForm.File {
			for _, header := range headers {
				item, err := adobeContentFromFile(key, header)
				if err != nil {
					return nil, err
				}
				content = append(content, item)
			}
		}
	}
	payload["messages"] = []any{map[string]any{"role": "user", "content": content}}
	return payload, nil
}

func adobeContentFromFile(key string, header *multipart.FileHeader) (map[string]any, error) {
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (100<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 100<<20 {
		return nil, fmt.Errorf("参考文件 %s 超过 100MB", header.Filename)
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = http.DetectContentType(raw)
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	lowerKey := strings.ToLower(key + " " + mimeType)
	switch {
	case strings.Contains(lowerKey, "audio"):
		return map[string]any{"type": "audio_url", "audio_url": map[string]any{"url": dataURL}}, nil
	case strings.Contains(lowerKey, "video"):
		return map[string]any{"type": "video_url", "video_url": map[string]any{"url": dataURL}}, nil
	default:
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL}}, nil
	}
}

func promptFromAdobeMessages(value any) string {
	messages, _ := value.([]any)
	for i := len(messages) - 1; i >= 0; i-- {
		message, _ := messages[i].(map[string]any)
		content := message["content"]
		if text, ok := content.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		parts, _ := content.([]any)
		for _, raw := range parts {
			part, _ := raw.(map[string]any)
			if text := strings.TrimSpace(fmt.Sprint(part["text"])); text != "" {
				return text
			}
		}
	}
	return ""
}

func adobeChatVideoURL(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	content := fmt.Sprint(message["content"])
	match := adobeVideoURLPattern.FindStringSubmatch(content)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func adobeChatImageURL(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	match := adobeImageURLPattern.FindStringSubmatch(fmt.Sprint(message["content"]))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func adobeImageURLs(payload map[string]any) []string {
	data, _ := payload["data"].([]any)
	urls := make([]string, 0, len(data))
	for _, raw := range data {
		item, _ := raw.(map[string]any)
		if value := strings.TrimSpace(fmt.Sprint(item["url"])); value != "" {
			urls = append(urls, value)
		}
	}
	return urls
}

func adobeProviderGenerationID(payload map[string]any, urls []string) string {
	if value := strings.TrimSpace(fmt.Sprint(payload["id"])); value != "" && value != "<nil>" {
		return value
	}
	if len(urls) > 0 {
		return filepath.Base(strings.SplitN(urls[0], "?", 2)[0])
	}
	return newVideoJobID()
}

func adobeProviderInfo(payload map[string]any) (map[string]any, string) {
	provider, _ := payload["provider"].(map[string]any)
	if provider == nil {
		provider = map[string]any{"name": "adobe"}
	}
	accountID := strings.TrimSpace(fmt.Sprint(provider["account_id"]))
	if accountID == "<nil>" {
		accountID = ""
	}
	return provider, accountID
}

func adobeAspect(payload map[string]any) string {
	value := firstNonEmpty(fmt.Sprint(payload["aspect_ratio"]), fmt.Sprint(payload["ratio"]), fmt.Sprint(payload["size"]))
	if value == "<nil>" {
		return ""
	}
	return value
}

func rewriteAdobeURLs(value any, host string) {
	rewriteAdobeURLsWithBase(value, "http://"+host+"/adobe/generated/")
}

func rewriteAdobeURLsWithBase(value any, base string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok {
				typed[key] = rewriteAdobeURLString(text, base)
				continue
			}
			rewriteAdobeURLsWithBase(item, base)
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok {
				typed[index] = rewriteAdobeURLString(text, base)
				continue
			}
			rewriteAdobeURLsWithBase(item, base)
		}
	}
}

func rewriteAdobeURLString(text, base string) string {
	for _, prefix := range []string{"http://127.0.0.1:6001/generated/", "http://localhost:6001/generated/"} {
		text = strings.ReplaceAll(text, prefix, base)
	}
	return text
}

func adobeGatewayError(err error) string {
	if err == nil {
		return "Adobe2API 调用失败"
	}
	raw := strings.TrimSpace(err.Error())
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "no active tokens") || strings.Contains(lower, "no available tokens"):
		return "没有可用的 Adobe 账号，请在 Adobe Firefly 页面导入或启用账号后重试"
	case strings.Contains(lower, "quota exhausted") || strings.Contains(lower, "taste_exhausted"):
		return "Adobe 账号额度不足，系统已尝试切换其他账号"
	case strings.Contains(lower, "token invalid") || strings.Contains(lower, "token expired") || strings.Contains(lower, "authentication_error"):
		return "Adobe 登录令牌已失效，系统已尝试刷新对应账号；请在账号池查看刷新错误"
	case strings.Contains(lower, "451"):
		return "Adobe 请求被区域或网络策略拒绝（HTTP 451），请在 Adobe Firefly 页面检查代理设置"
	case strings.Contains(lower, "408") || strings.Contains(lower, "timeout_error") || strings.Contains(lower, "system under load") || strings.Contains(lower, "system_under_load"):
		return "Adobe 上游繁忙（system under load），系统已自动重试；请稍后再试"
	case strings.Contains(lower, "temporarily unavailable") || strings.Contains(lower, "upstream_unavailable"):
		return "Adobe 上游暂时不可用，请稍后重试，并检查账号状态或代理设置"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused"):
		return "Adobe2API Sidecar 未运行或连接失败，请在 Adobe Firefly 页面启动服务"
	default:
		return "Adobe2API 调用失败：" + raw
	}
}
