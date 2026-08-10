// Package service contains the Leonardo cookie pool orchestrator. It mirrors
// app/leonardo_service.py: rotate cookies, refresh fallback tokens, auto-
// disable on auth/balance failure, and run a generation end-to-end.
package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

// LeonardoPool wires the cookie store with the Leonardo client.
type LeonardoPool struct {
	store      *store.Store
	api        *leonardo.Client
	sessionsMu sync.Mutex
	sessions   map[int64]*managedLeonardoSession
}

// NewLeonardoPool constructs the orchestrator.
func NewLeonardoPool(st *store.Store, client *leonardo.Client) *LeonardoPool {
	return &LeonardoPool{store: st, api: client, sessions: make(map[int64]*managedLeonardoSession)}
}

// Client exposes the underlying Leonardo HTTP client. Used by the desktop
// layer to download assets through the same TLS-impersonating client we
// already use for image upload + GraphQL.
func (p *LeonardoPool) Client() *leonardo.Client {
	return p.api
}

// authFailCooldown matches AUTH_FAIL_COOLDOWN_SECONDS in Python.
const authFailCooldown = 300 * time.Second

const accountChangedMessage = "该浏览器会话已被 Leonardo 切换到另一个账号；原账号记录已保留并停用，请用独立浏览器配置文件重新登录原账号后更新"

// PublicError carries a status code + safe message for HTTP handlers.
type PublicError struct {
	Status  int
	Message string
}

func (e *PublicError) Error() string { return e.Message }

func newPublicError(status int, msg string) *PublicError {
	return &PublicError{Status: status, Message: msg}
}

// GenerateRequest collects parameters for a single generation attempt.
type GenerateRequest struct {
	Prompt             string
	N                  int
	ModelID            string
	AspectRatio        string
	ReferenceImageURLs []string
	ReferenceImageIDs  []string // pre-uploaded init image ids; merged with ReferenceImageURLs results
	ReferenceImages    []ReferenceImageInput
	SaveResults        *bool // nil = follow auto_save_images setting
	// ClientRequestID makes retries from VideoClaw/Infinite Canvas idempotent.
	// It is stored only as generation metadata, never exposed as a credential.
	ClientRequestID string
}

// ReferenceImageInput keeps browser-uploaded bytes inside the cookie attempt
// that will submit the generation. Leonardo init-image ids are account-scoped,
// so uploading first and rotating to a different account can make a valid
// reference unusable.
type ReferenceImageInput struct {
	Content []byte
	Ext     string
}

// GenerateResponse mirrors the OpenAI-compatible response shape.
type GenerateResponse struct {
	Created  int64                `json:"created"`
	Data     []GenerateDataItem   `json:"data"`
	Provider GenerateProviderMeta `json:"provider"`
}

// GenerateDataItem is one generated image URL entry.
type GenerateDataItem struct {
	URL string `json:"url"`
}

// GenerateProviderMeta describes which cookie/model handled the job.
type GenerateProviderMeta struct {
	Provider        string   `json:"provider"`
	GenerationID    string   `json:"generation_id"`
	UsedCookieID    int64    `json:"used_cookie_id"`
	AspectRatio     string   `json:"aspect_ratio"`
	ModelID         string   `json:"model_id"`
	SavedFiles      []string `json:"saved_files"`
	AutoSaveEnabled bool     `json:"auto_save_enabled"`
	SaveError       string   `json:"save_error,omitempty"`
}

// Generate runs the full pipeline: rotate cookies, upload references,
// create generation, poll, optionally save images, log result.
func (p *LeonardoPool) Generate(req GenerateRequest) (*GenerateResponse, error) {
	metadataJSON := generationMetadata(req.ClientRequestID)
	width, height := ResolveSize(req.AspectRatio)
	quantity := req.N
	if quantity < 1 {
		quantity = 1
	}
	if quantity > 4 {
		quantity = 4
	}

	cookies, err := p.store.ListActiveCookies()
	if err != nil {
		return nil, fmt.Errorf("service: list cookies: %w", err)
	}
	if len(cookies) == 0 {
		return nil, newPublicError(400, "账号池中没有可用账号")
	}

	var errs []string

	for _, cookie := range cookies {
		if p.shouldSkipCookieNow(cookie) {
			errs = append(errs, fmt.Sprintf("cookie#%d: cooldown (auth recently failed)", cookie.ID))
			continue
		}

		// Each cookie gets up to two attempts: the first may resolve a fresh
		// token from the cookie itself, the second falls back to forcing a
		// resolve again or recognising auth-only failures.
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
					errs = append(errs, fmt.Sprintf("cookie#%d: 暂时无法刷新登录会话", cookie.ID))
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

			// Upload reference images (max 3, matching Python). Pre-uploaded
			// IDs (from desktop drag-drop / file picker) are prepended so they
			// take priority when caller supplies both forms.
			var initImageIDs []string
			for _, id := range req.ReferenceImageIDs {
				if id == "" {
					continue
				}
				initImageIDs = append(initImageIDs, id)
				if len(initImageIDs) >= 3 {
					break
				}
			}

			remaining := 3 - len(initImageIDs)
			if remaining < 0 {
				remaining = 0
			}
			rawRefs := req.ReferenceImages
			if len(rawRefs) > remaining {
				rawRefs = rawRefs[:remaining]
			}
			uploadFailed := false
			for _, ref := range rawRefs {
				id, err := p.api.UploadImageBytes(token, ref.Content, ref.Ext)
				if err != nil {
					if isAuthError(err.Error()) && attempt == 0 {
						p.reportRejectedJWT(cookie, err.Error())
						uploadFailed = true
						break
					}
					if isAuthError(err.Error()) {
						p.reportRejectedJWT(cookie, err.Error())
					}
					errs = append(errs, fmt.Sprintf("账号#%d 上传参考图失败：%s", cookie.ID, err.Error()))
					uploadFailed = true
					break
				}
				initImageIDs = append(initImageIDs, id)
			}
			if uploadFailed {
				if attempt == 0 {
					continue
				}
				break
			}

			refs := req.ReferenceImageURLs
			remaining = 3 - len(initImageIDs)
			if remaining < 0 {
				remaining = 0
			}
			if len(refs) > remaining {
				refs = refs[:remaining]
			}
			for _, refURL := range refs {
				id, err := p.api.UploadImageURL(token, refURL)
				if err != nil {
					if isAuthError(err.Error()) && attempt == 0 {
						p.reportRejectedJWT(cookie, err.Error())
						uploadFailed = true
						break
					}
					if isAuthError(err.Error()) {
						p.reportRejectedJWT(cookie, err.Error())
					}
					errs = append(errs, fmt.Sprintf("账号#%d 上传参考图失败：%s", cookie.ID, err.Error()))
					uploadFailed = true
					break
				}
				initImageIDs = append(initImageIDs, id)
			}
			if uploadFailed {
				if attempt == 0 {
					continue
				}
				break
			}

			sdVersion, _ := p.store.GetSDVersion(req.ModelID)

			genID, err := p.api.CreateGeneration(token, leonardo.GenerateInput{
				Prompt:       req.Prompt,
				ModelID:      req.ModelID,
				Width:        width,
				Height:       height,
				Quantity:     quantity,
				InitImageIDs: initImageIDs,
				SDVersion:    sdVersion,
			})
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

			result := p.api.WaitForCompletion(token, genID, 300*time.Second, 2*time.Second)
			if !result.Success {
				if isAuthError(result.Error) && attempt == 0 {
					p.reportRejectedJWT(cookie, result.Error)
					continue
				}
				if isAuthError(result.Error) {
					p.reportRejectedJWT(cookie, result.Error)
				}
				if strings.Contains(strings.ToLower(result.Error), "timeout") {
					_ = p.store.AddProviderGenerationLog(
						"leonardo", genID, "", "image", metadataJSON, cookie.ID,
						req.ModelID, req.AspectRatio, req.Prompt,
						nil, nil, false, "pending", result.Error,
					)
					return nil, newPublicError(504, fmt.Sprintf("Leonardo 任务已提交但结果同步超时（任务 ID：%s）。已停止换号重提，避免重复扣费；可稍后从素材库刷新。", genID))
				}
				errs = append(errs, fmt.Sprintf("cookie#%d: %s", cookie.ID, result.Error))
				break
			}

			_ = p.store.MarkCookieUsed(cookie.ID)
			// Re-sync balance now that credits were spent, so the UI shows the
			// updated number after the post-generate cookies:changed event.
			p.refreshBalanceAfterUse(cookie.ID, token)

			autoSaveEnabled := false
			if v, _ := p.store.GetSetting("auto_save_images", "0"); v == "1" {
				autoSaveEnabled = true
			}
			if req.SaveResults != nil {
				autoSaveEnabled = *req.SaveResults
			}

			var savedFiles []string
			var saveErrMsg string
			if autoSaveEnabled && len(result.Images) > 0 {
				files, err := p.saveGeneratedImages(genID, result.Images)
				savedFiles = files
				if err != nil {
					saveErrMsg = err.Error()
				}
			}

			_ = p.store.AddProviderGenerationLog(
				"leonardo", genID, "", "image", metadataJSON, cookie.ID,
				req.ModelID, req.AspectRatio, req.Prompt,
				result.Images, savedFiles, autoSaveEnabled, "success", "",
			)

			items := make([]GenerateDataItem, 0, len(result.Images))
			for _, u := range result.Images {
				items = append(items, GenerateDataItem{URL: u})
			}
			return &GenerateResponse{
				Created: time.Now().Unix(),
				Data:    items,
				Provider: GenerateProviderMeta{
					GenerationID:    genID,
					UsedCookieID:    cookie.ID,
					AspectRatio:     req.AspectRatio,
					ModelID:         req.ModelID,
					SavedFiles:      orEmpty(savedFiles),
					AutoSaveEnabled: autoSaveEnabled,
					SaveError:       saveErrMsg,
				},
			}, nil
		}
	}

	detail := "所有 Leonardo 账号均生成失败。"
	if len(errs) > 0 {
		if len(errs) > 6 {
			errs = errs[:6]
		}
		detail = "所有 Leonardo 账号均生成失败：" + strings.Join(errs, "；")
	}
	return nil, newPublicError(503, detail)
}

func generationMetadata(clientRequestID string) string {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if clientRequestID == "" {
		return "{}"
	}
	payload, err := json.Marshal(map[string]string{"client_request_id": clientRequestID})
	if err != nil {
		return "{}"
	}
	return string(payload)
}

// refreshBalanceAfterUse re-fetches the cookie's balance right after a
// successful generation so the UI reflects spent credits immediately. The
// generate loop fetches balance BEFORE generating (to gate on >0 credits), so
// without this the stored balance would stay at its pre-generation value.
// Best-effort: any error is ignored because the generation itself succeeded.
func (p *LeonardoPool) refreshBalanceAfterUse(cookieID int64, token string) int64 {
	if info, err := p.api.GetUserInfo(token); err == nil {
		_ = p.store.UpdateCookieProfile(cookieID, info.ID, info.Email, info.Tokens)
		return info.Tokens
	}
	return -1
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (p *LeonardoPool) saveGeneratedImages(generationID string, urls []string) ([]string, error) {
	rawDir, _ := p.store.GetSetting("save_images_dir", "data/generated")
	dir := strings.TrimSpace(rawDir)
	if dir == "" {
		dir = "data/generated"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	saved := make([]string, 0, len(urls))
	ts := time.Now().Unix()
	for i, u := range urls {
		body, _, err := p.api.Download(u)
		if err != nil {
			return saved, err
		}
		ext := filepath.Ext(stripQuery(u))
		if ext == "" {
			ext = ".jpg"
		}
		name := fmt.Sprintf("%s_%d_%d%s", generationID, i+1, ts, ext)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return saved, err
		}
		saved = append(saved, path)
	}
	return saved, nil
}

func stripQuery(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// resolveToken implements the same priority order as Python _resolve_token:
//
//  1. Resolve from cookie payload via Leonardo client (always preferred).
//  2. Use parsed `token=...` line if it is fresh + likely Leonardo.
//  3. Use raw value as JWT if it qualifies.

// ResolveToken resolves a usable bearer JWT from a raw stored auth value
// (the "cookie=...\ntoken=..." format the desktop app persists) using the
// given Leonardo client. It reuses the exact desktop resolution path and
// touches no store, so the mobile binding can reproduce desktop behaviour
// without a database.
func ResolveToken(api *leonardo.Client, rawAuthValue string) string {
	return (&LeonardoPool{api: api}).resolveToken(rawAuthValue)
}

func (p *LeonardoPool) resolveToken(rawAuthValue string) string {
	value := strings.TrimSpace(rawAuthValue)
	token, cookie := extractAuthParts(value)

	cookiePayload := strings.TrimSpace(cookie)
	if cookiePayload == "" {
		cookiePayload = value
	}
	if strings.HasPrefix(strings.ToLower(cookiePayload), "cookie:") {
		cookiePayload = strings.TrimSpace(cookiePayload[len("cookie:"):])
	}

	if cookiePayload != "" {
		if t := p.api.GetTokenFromCookie(cookiePayload); t != "" && leonardo.LooksLikeJWT(t) {
			return t
		}
	}

	// A token copied from an authenticated Leonardo request is later validated
	// against Leonardo GraphQL. Do not hard-code Cognito-only issuer heuristics:
	// Leonardo has changed its web authentication provider before.
	if leonardo.IsFreshToken(token, 120) && leonardo.LooksLikeJWT(token) {
		return token
	}
	if leonardo.IsFreshToken(value, 120) && leonardo.IsLikelyLeonardoToken(value) {
		return value
	}
	return ""
}

// resolveTokenForCookie resolves a token that belongs to the row's stored
// Leonardo user id. A copied browser session can later be switched remotely to
// another account; in that case the saved short-lived fallback token may still
// represent the original account, while the browser Cookie represents the new
// one. Prefer the identity-matching candidate and report a mismatch instead of
// ever running a job on the wrong account.
func (p *LeonardoPool) resolveTokenForCookie(cookie store.Cookie) (token string, accountChanged bool) {
	// Pure resolver compatibility for unit/mobile callers that intentionally do
	// not attach a database-backed account pool.
	if p.store == nil {
		expected := strings.TrimSpace(cookie.AccountID)
		parsedToken, parsedCookie := extractAuthParts(cookie.Value)
		accept := func(candidate string) (string, bool) {
			candidate = strings.TrimSpace(candidate)
			if !leonardo.IsFreshToken(candidate, 120) || !leonardo.LooksLikeJWT(candidate) {
				return "", false
			}
			resolved := strings.TrimSpace(leonardo.UserIDFromToken(candidate))
			if expected == "" || resolved == "" || resolved == expected {
				return candidate, false
			}
			return "", true
		}
		if candidate, mismatch := accept(parsedToken); candidate != "" || mismatch {
			return candidate, mismatch
		}
		if cookiePayload := normalizeStoredCookiePayload(parsedCookie, cookie.Value); cookiePayload != "" && p.api != nil {
			return accept(p.api.GetTokenFromCookie(cookiePayload))
		}
		return "", false
	}
	result := p.resolveTokenForCookieAttempt(cookie, false)
	return result.Token, result.AccountChanged
}

// refreshFallbackToken follows leo2api's split storage model: the browser
// Cookie remains the durable credential while the short-lived JWT and expiry
// live in dedicated columns.
func (p *LeonardoPool) refreshFallbackToken(cookieID int64, rawAuthValue, resolvedToken string) {
	_, parsedCookie := extractAuthParts(rawAuthValue)
	cookiePayload := strings.TrimSpace(parsedCookie)
	if cookiePayload == "" {
		raw := strings.TrimSpace(rawAuthValue)
		if strings.Contains(raw, ";") && strings.Contains(raw, "=") {
			cookiePayload = raw
		}
	}
	if cookiePayload == "" {
		return
	}

	current := strings.TrimSpace(rawAuthValue)
	next := cookiePayload
	if next != "" && next != current {
		_, _ = p.store.UpdateCookieValue(cookieID, next)
	}
	if leonardo.LooksLikeJWT(resolvedToken) {
		_ = p.store.UpdateCookieSessionSuccess(cookieID, resolvedToken, leonardo.TokenExp(resolvedToken))
	}
}

func composeStoreAuthValue(cookiePayload, token string) string {
	normalized := strings.TrimSpace(cookiePayload)
	if strings.HasPrefix(strings.ToLower(normalized), "cookie:") {
		normalized = strings.TrimSpace(normalized[len("cookie:"):])
	}
	tokenTrim := strings.TrimSpace(token)
	if tokenTrim != "" && leonardo.IsFreshToken(tokenTrim, 300) && leonardo.LooksLikeJWT(tokenTrim) {
		if normalized == "" {
			// A GraphQL cURL can carry the short-lived web Bearer token without
			// sending the app-domain session cookie to api.leonardo.ai. Keep the
			// validated token so the account remains usable until it expires.
			return "token=" + tokenTrim
		}
		return fmt.Sprintf("cookie=%s\ntoken=%s", normalized, tokenTrim)
	}
	return normalized
}

// extractAuthParts mirrors Python's _extract_auth_parts: parses lines like
// `cookie=...`, `token=...`, raw cookie strings, or bare JWTs.
func extractAuthParts(rawAuthValue string) (token, cookie string) {
	raw := strings.TrimSpace(rawAuthValue)
	if raw == "" {
		return "", ""
	}
	// Chrome/Edge "Copy as cURL" is the easiest way for a user to copy the
	// complete request Cookie, including HttpOnly values. Accept the whole
	// command but retain only its Cookie header; unrelated headers and the URL
	// are never persisted.
	if curlToken, curlCookie := extractAuthFromCurl(raw); curlToken != "" || curlCookie != "" {
		return curlToken, curlCookie
	}

	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		lines = []string{raw}
	}

	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "cookie:") {
			line = strings.TrimSpace(line[len("cookie:"):])
		}

		if strings.Contains(line, "=") {
			key, value, _ := strings.Cut(line, "=")
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)

			switch {
			case key == "token" && value != "":
				token = value
				continue
			case key == "cookie" && value != "":
				cookie = value
				continue
			}

			if strings.Contains(line, ";") {
				cookie = line
				continue
			}

			if strings.Contains(key, "next-auth") ||
				strings.HasPrefix(key, "__host-next-auth") ||
				strings.HasPrefix(key, "__secure-next-auth") ||
				strings.Contains(key, "better-auth") ||
				strings.HasPrefix(key, "__secure-better-auth") ||
				strings.Contains(key, "authjs") ||
				strings.HasPrefix(key, "__secure-authjs") ||
				strings.HasPrefix(key, "__host-authjs") {
				cookie = line
				continue
			}
		} else {
			if strings.Count(line, ".") == 2 && !strings.Contains(line, " ") && len(line) > 40 {
				token = line
				continue
			}
			if strings.Contains(line, ";") && strings.Contains(line, "=") {
				cookie = line
			}
		}
	}
	return token, cookie
}

func extractCookieHeaderFromCurl(raw string) string {
	_, cookie := extractAuthFromCurl(raw)
	return cookie
}

// extractAuthFromCurl accepts the common Chrome/Edge "Copy as cURL" forms:
//
//	-H 'cookie: name=value; ...'
//	-H 'authorization: Bearer ey...'
//	-b 'name=value; ...'
//	--cookie='name=value; ...'
//
// Only the Cookie and Bearer token are returned. The URL and all unrelated
// headers are discarded and never persisted.
func extractAuthFromCurl(raw string) (token, cookie string) {
	args := splitShellArgs(raw)
	if len(args) == 0 {
		return "", ""
	}
	command := strings.ToLower(strings.TrimSpace(args[0]))
	if command != "curl" && command != "curl.exe" {
		return "", ""
	}

	readValue := func(i *int, current, longName string) string {
		if strings.HasPrefix(current, longName+"=") {
			return strings.TrimSpace(current[len(longName)+1:])
		}
		if *i+1 < len(args) {
			*i = *i + 1
			return strings.TrimSpace(args[*i])
		}
		return ""
	}
	consumeHeader := func(header string) {
		name, value, ok := strings.Cut(header, ":")
		if !ok {
			return
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "cookie":
			value = strings.TrimSpace(value)
			if strings.Contains(value, "=") {
				cookie = value
			}
		case "authorization":
			value = strings.TrimSpace(value)
			if strings.HasPrefix(strings.ToLower(value), "bearer ") {
				candidate := strings.TrimSpace(value[len("bearer "):])
				if leonardo.LooksLikeJWT(candidate) {
					token = candidate
				}
			}
		}
	}

	for i := 1; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch {
		case lower == "-h" || lower == "--header" || strings.HasPrefix(lower, "--header="):
			consumeHeader(readValue(&i, arg, "--header"))
		case strings.HasPrefix(lower, "-h") && len(arg) > 2:
			consumeHeader(strings.TrimSpace(arg[2:]))
		case lower == "-b" || lower == "--cookie" || strings.HasPrefix(lower, "--cookie="):
			value := readValue(&i, arg, "--cookie")
			if strings.Contains(value, "=") && !strings.HasPrefix(value, "@") {
				cookie = value
			}
		case strings.HasPrefix(lower, "-b") && len(arg) > 2:
			value := strings.TrimSpace(arg[2:])
			if strings.Contains(value, "=") && !strings.HasPrefix(value, "@") {
				cookie = value
			}
		}
	}
	return token, cookie
}

// splitShellArgs is a deliberately small cURL tokenizer. It supports the
// quoting and line-continuation styles emitted by Chrome/Edge on bash, cmd,
// and PowerShell without executing or otherwise interpreting the command.
func splitShellArgs(raw string) []string {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}
	runes := []rune(strings.TrimSpace(raw))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			if r != '\r' && r != '\n' {
				current.WriteRune(r)
			}
			escaped = false
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			current.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '\\', '^', '`':
			if i+1 < len(runes) && (runes[i+1] == '\r' || runes[i+1] == '\n') {
				escaped = true
				continue
			}
			if r == '\\' {
				escaped = true
			} else {
				current.WriteRune(r)
			}
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
	return args
}

// shouldSkipCookieNow follows the imported leo2api scheduler state. Explicit
// per-account error_until takes precedence; the old five-minute auth cooldown
// remains only as a migration fallback for rows created by earlier builds.
func (p *LeonardoPool) shouldSkipCookieNow(c store.Cookie) bool {
	status := strings.ToLower(strings.TrimSpace(c.SessionStatus))
	if status == leoInvalidStatus || status == leoAbnormalStatus {
		return true
	}
	// error_until throttles get-session refreshes, not generation itself. Like
	// leo2api, keep scheduling an account while its cached JWT remains valid; a
	// proactive 429/challenge/network failure must not waste the remaining JWT.
	if leonardo.IsFreshToken(strings.TrimSpace(c.JWTToken), 0) {
		return false
	}
	if c.ErrorUntil > time.Now().Unix() {
		return true
	}
	if !isAuthError(c.LastError) {
		return false
	}
	if c.LastCheckedAt == 0 {
		return false
	}
	return time.Since(time.Unix(c.LastCheckedAt, 0)) < authFailCooldown
}

func (p *LeonardoPool) disableCookie(id int64, reason, message string) {
	_ = p.store.MarkCookieError(id, message)
	_ = p.store.AutoDisableCookie(id, reason)
}

// ensureCookieIdentity prevents a browser session that was switched remotely
// to account B from silently mutating a saved account-A row. This invariant is
// essential for a durable multi-account pool: account identity may be adopted
// once for legacy rows, but it must never drift afterward.
func (p *LeonardoPool) ensureCookieIdentity(cookie store.Cookie, info leonardo.UserInfo) error {
	expected := strings.TrimSpace(cookie.AccountID)
	resolved := strings.TrimSpace(info.ID)
	if expected == "" || resolved == "" || expected == resolved {
		return nil
	}
	p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
	return errors.New(accountChangedMessage)
}

func isAuthError(message string) bool {
	text := strings.TrimSpace(strings.ToLower(message))
	if text == "" {
		return false
	}
	markers := []string{
		"jwt expired",
		"token expired",
		"invalid token",
		"invalid bearer",
		"unauthorized",
		"forbidden",
		"access denied",
		"401",
		"403",
		"session refresh gagal",
		"failed token",
		"failed to fetch token",
		"auth tidak valid",
		"authentication",
	}
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// AddCookieValidated mirrors Python add_cookie_validated: only persist a
// cookie that resolves to a usable JWT and report the balance back.
func (p *LeonardoPool) AddCookieValidated(rawAuthValue string) (UserInfoResult, error) {
	storeValue, info, err := p.validateAndPrepareCookie(rawAuthValue)
	if err != nil {
		return UserInfoResult{}, err
	}
	upsert, err := p.store.UpsertCookieAccount(storeValue, info.ID, info.Email, info.Tokens)
	if err != nil {
		return UserInfoResult{}, err
	}
	if info.Tokens > 0 {
		_ = p.store.MarkCookieUsed(upsert.ID)
	}
	if token, _ := extractAuthParts(storeValue); leonardo.LooksLikeJWT(token) {
		p.refreshFallbackToken(upsert.ID, storeValue, token)
	}
	return UserInfoResult{
		AccountID:        info.ID,
		Email:            info.Email,
		Balance:          info.Tokens,
		UpdatedExisting:  upsert.UpdatedExisting,
		MergedDuplicates: upsert.Merged,
	}, nil
}

// RefreshBoundCookieValidated refreshes only the account a managed browser
// workspace was originally bound to. If that Chrome profile has since been
// switched to another Leonardo login, no pool row is created or overwritten.
func (p *LeonardoPool) RefreshBoundCookieValidated(expectedAccountID, rawAuthValue string) (UserInfoResult, error) {
	expectedAccountID = strings.TrimSpace(expectedAccountID)
	if expectedAccountID == "" {
		return UserInfoResult{}, errors.New("Leonardo 工作区尚未绑定账号，请先在桌面账号池中执行一次“一键读取 Cookie”")
	}
	storeValue, info, err := p.validateAndPrepareCookie(rawAuthValue)
	if err != nil {
		return UserInfoResult{}, err
	}
	if resolved := strings.TrimSpace(info.ID); resolved == "" || resolved != expectedAccountID {
		return UserInfoResult{}, errors.New("该 Chrome 工作区当前登录的账号与原绑定账号不同，已拒绝自动更新；请重新登录原账号")
	}
	upsert, err := p.store.UpsertCookieAccount(storeValue, info.ID, info.Email, info.Tokens)
	if err != nil {
		return UserInfoResult{}, err
	}
	if token, _ := extractAuthParts(storeValue); leonardo.LooksLikeJWT(token) {
		p.refreshFallbackToken(upsert.ID, storeValue, token)
	}
	if info.Tokens > 0 {
		_ = p.store.MarkCookieUsed(upsert.ID)
	}
	return UserInfoResult{
		AccountID: info.ID, Email: info.Email, Balance: info.Tokens,
		UpdatedExisting: upsert.UpdatedExisting, MergedDuplicates: upsert.Merged,
	}, nil
}

// UpdateCookieValidated replaces an existing cookie's payload after running
// the same validation as AddCookieValidated. Used when the user pastes a
// fresh cookie to an existing slot (e.g. after logging in again).
func (p *LeonardoPool) UpdateCookieValidated(id int64, rawAuthValue string) (UserInfoResult, error) {
	storeValue, info, err := p.validateAndPrepareCookie(rawAuthValue)
	if err != nil {
		return UserInfoResult{}, err
	}
	existing, err := p.store.GetCookieByID(id)
	if err != nil {
		return UserInfoResult{}, err
	}
	if existing == nil {
		return UserInfoResult{}, errors.New("要更新的账号不存在")
	}
	if expected, resolved := strings.TrimSpace(existing.AccountID), strings.TrimSpace(info.ID); expected != "" && resolved != "" && expected != resolved {
		return UserInfoResult{}, errors.New("不能用另一个 Leonardo 账号的 Cookie 覆盖当前账号；请回到“添加账号”区域新增")
	}
	changed, err := p.store.UpdateCookieValue(id, storeValue)
	if err != nil {
		return UserInfoResult{}, err
	}
	if !changed {
		return UserInfoResult{}, errors.New("该 Cookie 已经导入，无需重复保存")
	}
	_ = p.store.UpdateCookieProfile(id, info.ID, info.Email, info.Tokens)
	if token, _ := extractAuthParts(storeValue); leonardo.LooksLikeJWT(token) {
		p.refreshFallbackToken(id, storeValue, token)
	}
	// Re-enable when the operator pasted a fresh cookie into a disabled slot.
	if info.Tokens > 0 {
		_ = p.store.ToggleCookie(id, true)
		_ = p.store.MarkCookieUsed(id)
	}
	return UserInfoResult{AccountID: info.ID, Email: info.Email, Balance: info.Tokens}, nil
}

// validateAndPrepareCookie centralises the cookie validation pipeline used
// by both Add and Update flows. It returns the canonical store value plus
// the resolved user info.
func (p *LeonardoPool) validateAndPrepareCookie(rawAuthValue string) (string, leonardo.UserInfo, error) {
	value := strings.TrimSpace(rawAuthValue)
	if value == "" {
		return "", leonardo.UserInfo{}, errors.New("Cookie 不能为空")
	}

	parsedToken, parsedCookie := extractAuthParts(value)
	isCopiedCurl := strings.HasPrefix(strings.ToLower(value), "curl")
	cookiePayload := strings.TrimSpace(parsedCookie)
	if strings.HasPrefix(strings.ToLower(cookiePayload), "cookie:") {
		cookiePayload = strings.TrimSpace(cookiePayload[len("cookie:"):])
	}

	if cookiePayload == "" || !strings.Contains(cookiePayload, "=") {
		if isCopiedCurl && parsedToken != "" && leonardo.IsFreshToken(parsedToken, 120) && leonardo.LooksLikeJWT(parsedToken) {
			info, err := p.api.GetUserInfo(parsedToken)
			if err != nil {
				lowerErr := strings.ToLower(err.Error())
				if strings.Contains(lowerErr, "jwt") || strings.Contains(lowerErr, "unauthorized") || strings.Contains(lowerErr, "authentication") {
					return "", leonardo.UserInfo{}, errors.New("cURL 中的 Authorization 令牌无效或已过期；请刷新 Leonardo 页面后重新复制 GraphQL 请求")
				}
				return "", leonardo.UserInfo{}, err
			}
			return composeStoreAuthValue("", parsedToken), info, nil
		}
		if parsedToken != "" || leonardo.LooksLikeJWT(value) {
			return "", leonardo.UserInfo{}, errors.New("不接受单独 JWT，请粘贴浏览器中的完整 Cookie 字符串")
		}
		return "", leonardo.UserInfo{}, errors.New("Cookie 格式无效，需要完整的 name=value; ... 字符串")
	}

	lower := strings.ToLower(cookiePayload)
	hasMarker := strings.Contains(lower, "next-auth.session-token") ||
		strings.Contains(lower, "authjs.session-token") ||
		strings.Contains(lower, "__secure-next-auth.session-token") ||
		strings.Contains(lower, "__secure-authjs.session-token") ||
		strings.Contains(lower, "__host-next-auth.csrf-token") ||
		strings.Contains(lower, "next-auth.csrf-token") ||
		strings.Contains(lower, "better-auth.session_token") ||
		strings.Contains(lower, "better-auth.session-token") ||
		strings.Contains(lower, "better-auth.session_data")
	if !hasMarker && !(isCopiedCurl && parsedToken != "" && leonardo.IsFreshToken(parsedToken, 120) && leonardo.LooksLikeJWT(parsedToken)) {
		return "", leonardo.UserInfo{}, errors.New("Cookie 不是有效的 Leonardo 登录会话，请登录后重新获取")
	}

	token := ""
	if parsedToken != "" {
		candidate := strings.TrimSpace(parsedToken)
		if leonardo.IsFreshToken(candidate, 120) && leonardo.LooksLikeJWT(candidate) {
			token = candidate
		}
	}
	if token == "" {
		refreshed, refreshErr := p.api.RefreshSession(cookiePayload)
		if strings.TrimSpace(refreshed.Cookie) != "" {
			cookiePayload = refreshed.Cookie
		}
		if refreshErr == nil {
			token = refreshed.Token
		} else {
			// Keep the previous next-auth compatibility path for genuinely old
			// sessions, but Better Auth imports use the rotating-cookie result.
			var report leonardo.TokenResolutionReport
			token, report = p.api.GetTokenFromCookieWithReport(cookiePayload)
			if token == "" {
				if sessionErr, ok := refreshErr.(*leonardo.SessionRefreshError); ok {
					switch sessionErr.Kind {
					case leonardo.SessionErrorRateLimit, leonardo.SessionErrorChallenge, leonardo.SessionErrorTransient:
						return "", leonardo.UserInfo{}, fmt.Errorf("Leonardo 登录会话暂时无法验证：%s", sessionErr.Error())
					}
				}
				return "", leonardo.UserInfo{}, errors.New(tokenResolutionError(parsedToken, report))
			}
		}
	}
	if token == "" && parsedToken != "" {
		candidate := strings.TrimSpace(parsedToken)
		if leonardo.IsFreshToken(candidate, 300) && leonardo.LooksLikeJWT(candidate) {
			token = candidate
		}
	}
	if strings.Count(token, ".") != 2 {
		return "", leonardo.UserInfo{}, errors.New("会话令牌无法用于 Leonardo 内部接口")
	}

	info, err := p.api.GetUserInfo(token)
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if strings.Contains(lowerErr, "jwt") ||
			strings.Contains(lowerErr, "unauthorized") ||
			strings.Contains(lowerErr, "invalid bearer") ||
			strings.Contains(lowerErr, "authentication") {
			return "", leonardo.UserInfo{}, errors.New("cURL 中的 Authorization 令牌无效或已过期；请刷新 Leonardo 页面后重新复制 GraphQL 请求")
		}
		if strings.Contains(lowerErr, "dial tcp") ||
			strings.Contains(lowerErr, "timeout") ||
			strings.Contains(lowerErr, "connection") {
			return "", leonardo.UserInfo{}, errors.New("无法连接 Leonardo API；请确认 Windows 系统代理正在运行后重试")
		}
		return "", leonardo.UserInfo{}, err
	}

	storeValue := composeStoreAuthValue(cookiePayload, token)
	if storeValue == "" {
		storeValue = composeStoreAuthValue(cookiePayload, parsedToken)
	}
	return storeValue, info, nil
}

func tokenResolutionError(parsedToken string, report leonardo.TokenResolutionReport) string {
	if parsedToken != "" && leonardo.LooksLikeJWT(strings.TrimSpace(parsedToken)) && !leonardo.IsFreshToken(strings.TrimSpace(parsedToken), 120) {
		return "cURL 中已识别到 Authorization 令牌，但它已过期；请刷新 Leonardo 页面后重新复制 GraphQL 请求"
	}
	switch report.BetterAuthStatus {
	case 401, 403:
		return fmt.Sprintf("Cookie 已识别，但 Leonardo 会话接口返回 HTTP %d；请重新登录，并复制 api.leonardo.ai/v1/graphql 请求的 cURL", report.BetterAuthStatus)
	case 429:
		return "Leonardo 会话接口触发安全检查（HTTP 429）；请复制 api.leonardo.ai/v1/graphql 请求的 cURL，本地程序会同时读取 Authorization 和 Cookie"
	case 200:
		return "Cookie 会话有效但响应中没有可用访问令牌；请复制 api.leonardo.ai/v1/graphql 请求的 cURL，而不是图片或静态资源请求"
	}
	if len(report.CookieNames) > 0 {
		return "已识别 Cookie，但无法换取访问令牌；请复制 api.leonardo.ai/v1/graphql 请求的 cURL（不要选择图片或静态资源）"
	}
	return "登录会话无效：无法获取访问令牌"
}

// UserInfoResult is the public payload returned by AddCookieValidated.
type UserInfoResult struct {
	AccountID        string
	Email            string
	Balance          int64
	UpdatedExisting  bool
	MergedDuplicates int
}

// helpers retained to satisfy package imports
var _ = json.RawMessage{}
var _ = base64.StdEncoding
var _ = url.QueryEscape
