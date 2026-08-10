package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
)

const (
	providerAdobe = "adobe"
	providerLeo   = "leo"

	leoBaseURLSetting   = "leo_base_url"
	leoAPIKeySetting    = "leo_api_key"
	leoEnabledSetting   = "leo_enabled"
	adobeBaseURLSetting = "adobe_base_url"
	adobeAPIKeySetting  = "adobe_api_key"
	adobeEnabledSetting = "adobe_enabled"
)

// ProviderConfigDTO intentionally never returns the raw API key to the UI.
// The key stays in the local SQLite settings table and is only attached to
// outbound requests by the Go process.
type ProviderConfigDTO struct {
	Provider         string   `json:"provider"`
	Name             string   `json:"name"`
	BaseURL          string   `json:"baseURL"`
	Enabled          bool     `json:"enabled"`
	APIKeyConfigured bool     `json:"apiKeyConfigured"`
	APIKeyMasked     string   `json:"apiKeyMasked"`
	Capabilities     []string `json:"capabilities"`
}

type ProviderConfigInput struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"baseURL"`
	APIKey      string `json:"apiKey"`
	Enabled     bool   `json:"enabled"`
	ClearAPIKey bool   `json:"clearApiKey"`
}

type ProviderConnectionResult struct {
	Provider   string `json:"provider"`
	Reachable  bool   `json:"reachable"`
	HTTPStatus int    `json:"httpStatus"`
	Message    string `json:"message"`
	ModelCount int    `json:"modelCount"`
	CheckedAt  int64  `json:"checkedAt"`
}

type providerConfig struct {
	Provider string
	Name     string
	BaseURL  string
	APIKey   string
	Enabled  bool
}

func (a *App) initializeProviderSettings() {
	defaults := []struct {
		key   string
		value string
	}{
		{leoBaseURLSetting, "http://127.0.0.1:8787"},
		{leoAPIKeySetting, discoverLeoAPIKey()},
		{leoEnabledSetting, "1"},
		{adobeBaseURLSetting, a.adobe.Client.BaseURL},
		{adobeAPIKeySetting, discoverAdobeAPIKey(a)},
		{adobeEnabledSetting, "1"},
	}
	for _, item := range defaults {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		if current, err := a.store.GetSetting(item.key, ""); err == nil && strings.TrimSpace(current) == "" {
			if err := a.store.SetSetting(item.key, item.value); err != nil {
				logProviderError(item.key, err)
			}
		}
	}
}

func logProviderError(key string, err error) {
	if err != nil {
		// Keep initialization best-effort. The interface page can still repair
		// the setting manually when an old database is read-only.
		fmt.Printf("provider setting %s: %v\n", key, err)
	}
}

func discoverAdobeAPIKey(a *App) string {
	if a == nil || a.adobe == nil || a.adobe.Client == nil {
		return strings.TrimSpace(os.Getenv("ADOBE2API_API_KEY"))
	}
	if cfg, err := a.adobe.Client.LoadConfig(); err == nil {
		if value := strings.TrimSpace(cfg.APIKey); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv("ADOBE2API_API_KEY"))
}

func discoverLeoAPIKey() string {
	if value := strings.TrimSpace(os.Getenv("LEO2API_API_KEY")); value != "" {
		return value
	}
	paths := []string{}
	if configured := strings.TrimSpace(os.Getenv("LEO2API_CONFIG_PATH")); configured != "" {
		paths = append(paths, configured)
	}
	for _, base := range executableAndWorkingBases() {
		paths = append(paths,
			filepath.Join(base, "leo2api-main", "leo2api-main", "config", "config.json"),
			filepath.Join(base, "leo2api-main", "config", "config.json"),
		)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			APIKey string `json:"api_key"`
		}
		if json.Unmarshal(raw, &cfg) == nil && strings.TrimSpace(cfg.APIKey) != "" {
			return strings.TrimSpace(cfg.APIKey)
		}
	}
	return ""
}

func executableAndWorkingBases() []string {
	seen := map[string]struct{}{}
	result := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if abs, err := filepath.Abs(value); err == nil {
			value = abs
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if cwd, err := os.Getwd(); err == nil {
		for current := cwd; current != filepath.Dir(current); current = filepath.Dir(current) {
			add(current)
		}
		add(filepath.Dir(cwd))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for current := dir; current != filepath.Dir(current); current = filepath.Dir(current) {
			add(current)
		}
		add(filepath.Dir(dir))
	}
	return result
}

func (a *App) loadProviderConfig(provider string) (providerConfig, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	var out providerConfig
	switch provider {
	case providerLeo:
		out = providerConfig{Provider: providerLeo, Name: "Leo2API", BaseURL: "http://127.0.0.1:8787", Enabled: true}
		out.BaseURL, _ = a.store.GetSetting(leoBaseURLSetting, out.BaseURL)
		out.APIKey, _ = a.store.GetSetting(leoAPIKeySetting, "")
		enabled, _ := a.store.GetSetting(leoEnabledSetting, "1")
		out.Enabled = enabled == "1" || strings.EqualFold(enabled, "true")
	case providerAdobe:
		base := "http://127.0.0.1:6001"
		if a.adobe != nil && a.adobe.Client != nil && strings.TrimSpace(a.adobe.Client.BaseURL) != "" {
			base = a.adobe.Client.BaseURL
		}
		out = providerConfig{Provider: providerAdobe, Name: "Adobe2API", BaseURL: base, Enabled: true}
		out.BaseURL, _ = a.store.GetSetting(adobeBaseURLSetting, out.BaseURL)
		out.APIKey, _ = a.store.GetSetting(adobeAPIKeySetting, discoverAdobeAPIKey(a))
		enabled, _ := a.store.GetSetting(adobeEnabledSetting, "1")
		out.Enabled = enabled == "1" || strings.EqualFold(enabled, "true")
	default:
		return out, fmt.Errorf("不支持的接口提供商：%s", provider)
	}
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	if out.BaseURL == "" {
		return out, errors.New("接口地址不能为空")
	}
	return out, nil
}

func maskAPIKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return strings.Repeat("•", len(value))
	}
	return value[:4] + strings.Repeat("•", len(value)-8) + value[len(value)-4:]
}

func providerDTO(cfg providerConfig) ProviderConfigDTO {
	capabilities := []string{"models"}
	if cfg.Provider == providerLeo {
		capabilities = []string{"image", "video", "models"}
	} else {
		capabilities = []string{"image", "video", "models"}
	}
	return ProviderConfigDTO{
		Provider: cfg.Provider, Name: cfg.Name, BaseURL: cfg.BaseURL,
		Enabled: cfg.Enabled, APIKeyConfigured: strings.TrimSpace(cfg.APIKey) != "",
		APIKeyMasked: maskAPIKey(cfg.APIKey), Capabilities: capabilities,
	}
}

func (a *App) ListProviderConfigs() ([]ProviderConfigDTO, error) {
	leo, err := a.loadProviderConfig(providerLeo)
	if err != nil {
		return nil, err
	}
	adobe, err := a.loadProviderConfig(providerAdobe)
	if err != nil {
		return nil, err
	}
	return []ProviderConfigDTO{providerDTO(adobe), providerDTO(leo)}, nil
}

func (a *App) SaveProviderConfig(input ProviderConfigInput) (*ProviderConfigDTO, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	cfg, err := a.loadProviderConfig(provider)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("接口地址必须是完整的 http:// 或 https:// 地址")
	}
	cfg.BaseURL = base
	cfg.Enabled = input.Enabled
	if input.ClearAPIKey {
		cfg.APIKey = ""
	} else if strings.TrimSpace(input.APIKey) != "" {
		cfg.APIKey = strings.TrimSpace(input.APIKey)
	}
	keys := map[string]string{}
	if provider == providerLeo {
		keys[leoBaseURLSetting] = cfg.BaseURL
		keys[leoAPIKeySetting] = cfg.APIKey
		if cfg.Enabled {
			keys[leoEnabledSetting] = "1"
		} else {
			keys[leoEnabledSetting] = "0"
		}
	} else {
		keys[adobeBaseURLSetting] = cfg.BaseURL
		keys[adobeAPIKeySetting] = cfg.APIKey
		if cfg.Enabled {
			keys[adobeEnabledSetting] = "1"
		} else {
			keys[adobeEnabledSetting] = "0"
		}
	}
	for key, value := range keys {
		if err := a.store.SetSetting(key, value); err != nil {
			return nil, err
		}
	}
	returnValue := providerDTO(cfg)
	return &returnValue, nil
}

func (a *App) TestProviderConnection(provider string) (*ProviderConnectionResult, error) {
	cfg, err := a.loadProviderConfig(provider)
	if err != nil {
		return nil, err
	}
	result := &ProviderConnectionResult{Provider: cfg.Provider, CheckedAt: time.Now().Unix()}
	if !cfg.Enabled {
		result.Message = "接口已禁用"
		return result, nil
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		result.Message = "未配置 API Key"
		return result, nil
	}
	path := "/health"
	if cfg.Provider == providerAdobe {
		path = "/api/v1/health"
	}
	client := providerHTTPClient(10 * time.Second)
	status, _, err := doProviderRequest(context.Background(), client, cfg, http.MethodGet, path, nil)
	result.HTTPStatus = status
	if err != nil {
		result.Message = err.Error()
		return result, nil
	}
	modelPayload, err := providerJSON(context.Background(), client, cfg, http.MethodGet, "/v1/models", nil)
	if err != nil {
		result.Message = err.Error()
		return result, nil
	}
	result.ModelCount = len(modelData(modelPayload))
	result.Reachable = true
	result.Message = fmt.Sprintf("连接正常，检测到 %d 个模型", result.ModelCount)
	return result, nil
}

func (a *App) providerModels(provider string) (map[string]any, providerConfig, error) {
	cfg, err := a.loadProviderConfig(provider)
	if err != nil {
		return nil, cfg, err
	}
	if !cfg.Enabled {
		return nil, cfg, fmt.Errorf("%s 接口已禁用", cfg.Name)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, cfg, fmt.Errorf("%s 尚未配置 API Key", cfg.Name)
	}
	payload, err := providerJSON(context.Background(), providerHTTPClient(10*time.Second), cfg, http.MethodGet, "/v1/models", nil)
	return payload, cfg, err
}

func modelData(payload map[string]any) []any {
	if payload == nil {
		return nil
	}
	items, _ := payload["data"].([]any)
	return items
}

func providerHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func providerJSON(ctx context.Context, client *http.Client, cfg providerConfig, method, path string, body any) (map[string]any, error) {
	status, payload, err := doProviderRequest(ctx, client, cfg, method, path, body)
	if err != nil {
		if status > 0 {
			return nil, fmt.Errorf("%s HTTP %d: %w", cfg.Name, status, err)
		}
		return nil, fmt.Errorf("%s: %w", cfg.Name, err)
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

func doProviderRequest(ctx context.Context, client *http.Client, cfg providerConfig, method, path string, body any) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.BaseURL, "/")+"/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var payload map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, payload, providerPayloadError(payload, strings.TrimSpace(string(raw)))
	}
	return resp.StatusCode, payload, nil
}

func providerPayloadError(payload map[string]any, raw string) error {
	if payload != nil {
		if detail, ok := payload["detail"].(string); ok && strings.TrimSpace(detail) != "" {
			return errors.New(detail)
		}
		if nested, ok := payload["error"].(map[string]any); ok {
			if message, ok := nested["message"].(string); ok && strings.TrimSpace(message) != "" {
				return errors.New(message)
			}
		}
		if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
			return errors.New(message)
		}
	}
	if raw == "" {
		raw = "请求失败"
	}
	return errors.New(raw)
}

func providerID(payload map[string]any) string {
	if payload == nil {
		return fmt.Sprintf("remote-%d", time.Now().UnixNano())
	}
	if value := strings.TrimSpace(fmt.Sprint(payload["id"])); value != "" && value != "<nil>" {
		return value
	}
	return fmt.Sprintf("remote-%d", time.Now().UnixNano())
}

func modelString(item map[string]any, key string) string {
	value := strings.TrimSpace(fmt.Sprint(item[key]))
	if value == "<nil>" {
		return ""
	}
	return value
}

func modelIntSlice(value any) []int {
	var out []int
	items, _ := value.([]any)
	for _, item := range items {
		if number, ok := item.(float64); ok {
			out = append(out, int(number))
			continue
		}
		if number, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(item))); err == nil {
			out = append(out, number)
		}
	}
	sort.Ints(out)
	return out
}

func leoModelDTOs(payload map[string]any) []VideoModelDTO {
	items := modelData(payload)
	out := make([]VideoModelDTO, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		id := modelString(item, "id")
		if id == "" {
			continue
		}
		params, _ := item["parameters"].(map[string]any)
		durations := modelIntSlice(params["duration"])
		if len(durations) == 0 {
			durations = []int{8}
		}
		sizes, _ := params["size"].([]any)
		modes := []string{"RESOLUTION_720"}
		for _, rawSize := range sizes {
			size := strings.ToLower(strings.TrimSpace(fmt.Sprint(rawSize)))
			if strings.Contains(size, "864x") || strings.Contains(size, "496x") {
				modes = []string{"RESOLUTION_480"}
				break
			}
		}
		name := modelString(item, "display_name")
		if name == "" {
			name = strings.ReplaceAll(id, "-", " ")
		}
		family := "Leo2API"
		if strings.HasPrefix(id, "video-2.0") {
			family = "Seedance 2.0"
		} else if id == "sora2" {
			family = "Sora 2"
		} else if id == "ko3" {
			family = "Kling O3"
		} else if id == "minimax-h3" {
			family = "MiniMax H3"
		}
		out = append(out, VideoModelDTO{
			Name: name, Family: family, Slug: id, ModelValue: id,
			RequestProfile: "leo2api", DefaultMode: modes[0], SupportedModes: modes,
			DurationOptions: durations, DefaultDuration: durations[0],
			SupportsAudio: true, AudioPolicy: "optional", SupportsRefImage: true,
			SupportsImageReference: true, SupportsVideoReference: id == "ko3",
			DefaultAspect: "16:9", Notes: modelString(item, "description"),
		})
	}
	return out
}

func adobeImageModelDTOs(payload map[string]any) []ModelDTO {
	items := modelData(payload)
	out := make([]ModelDTO, 0, 12)
	seen := map[string]struct{}{}
	preferredRatios := []string{"1x1", "16x9", "9x16", "4x3", "3x4"}
	for _, ratio := range preferredRatios {
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id := modelString(item, "id")
			if id == "" || !strings.Contains(id, "-2k-") || !strings.HasSuffix(id, "-"+ratio) || !strings.Contains(id, "nano-banana2") {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			name := modelString(item, "display_name")
			if name == "" {
				name = "Adobe 图片模型"
			}
			out = append(out, ModelDTO{ID: int64(len(out) + 1), Name: name + " · 2K · " + strings.ReplaceAll(ratio, "x", ":"), ModelID: id, SDVersion: "Adobe2API", IsDefault: len(out) == 0, CreatedAt: time.Now().Unix()})
			break
		}
	}
	return out
}

func leoImageModelDTOs(payload map[string]any) []ModelDTO {
	items := modelData(payload)
	out := make([]ModelDTO, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if strings.ToLower(strings.TrimSpace(modelString(item, "kind"))) != "image" {
			continue
		}
		id := modelString(item, "id")
		if id == "" {
			continue
		}
		name := modelString(item, "display_name")
		if name == "" {
			name = "Leonardo 图片模型"
		}
		out = append(out, ModelDTO{
			ID: int64(len(out) + 1), Name: name + " · Leo2API",
			ModelID: "leo:" + id, SDVersion: modelString(item, "sd_version"),
			IsDefault: false, CreatedAt: time.Now().Unix(),
		})
	}
	return out
}

func mapLeoModel(value string) string {
	q := strings.ToLower(strings.TrimSpace(value))
	switch q {
	case "seedance-2.0", "seedance2", "seedance-2":
		return "video-2.0"
	case "seedance-2.0-fast", "seedance2-fast":
		return "video-2.0-fast"
	case "seedance-2.0-mini", "seedance2-mini":
		return "video-2.0-mini"
	case "seedance-2.0-480p":
		return "video-2.0-480p"
	case "seedance-2.0-fast-480p":
		return "video-2.0-fast-480p"
	case "seedance-2.0-mini-480p":
		return "video-2.0-mini-480p"
	case "sora-2":
		return "sora2"
	case "kling-o3", "kling-video-o-3":
		return "ko3"
	case "hailuo-03", "minimax-hailuo-03":
		return "minimax-h3"
	default:
		return strings.TrimSpace(value)
	}
}

func leoSize(model, aspect, resolution string) string {
	model = mapLeoModel(model)
	aspect = strings.TrimSpace(aspect)
	if strings.Contains(strings.ToLower(resolution), "480") || strings.Contains(model, "480p") {
		switch aspect {
		case "9:16":
			return "496x864"
		case "1:1":
			return "640x640"
		default:
			return "864x496"
		}
	}
	if model == "sora2" {
		if aspect == "9:16" {
			return "720x1280"
		}
		return "1280x720"
	}
	if model == "minimax-h3" {
		switch aspect {
		case "9:16":
			return "1440x2560"
		case "1:1":
			return "1440x1440"
		default:
			return "2560x1440"
		}
	}
	switch aspect {
	case "9:16":
		return "720x1280"
	case "1:1":
		return "960x960"
	default:
		return "1280x720"
	}
}

func pollLeoVideo(ctx context.Context, cfg providerConfig, id, pollURL string) (map[string]any, error) {
	path := pollURL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		parsed, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		path = parsed.Path
	}
	if strings.TrimSpace(path) == "" {
		path = "/v1/video/generations/" + id
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	for {
		payload, err := providerJSON(deadline, providerHTTPClient(20*time.Second), cfg, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		status := strings.ToLower(modelString(payload, "status"))
		switch status {
		case "succeeded", "completed", "complete", "success":
			return payload, nil
		case "failed", "error", "cancelled", "canceled":
			return nil, providerPayloadError(payload, "Leo2API 视频任务失败")
		}
		select {
		case <-deadline.Done():
			return nil, errors.New("Leo2API 视频生成超时")
		case <-time.After(2 * time.Second):
		}
	}
}

func leoVideoURLs(payload map[string]any) []string {
	items, _ := payload["data"].([]any)
	urls := []string{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if value := modelString(item, "url"); value != "" {
			urls = append(urls, value)
		}
		if value := modelString(item, "video_url"); value != "" {
			urls = append(urls, value)
		}
	}
	return urls
}

func (a *App) listRemoteImageModels() ([]ModelDTO, error) {
	var out []ModelDTO
	var firstErr error
	if payload, _, err := a.providerModels(providerAdobe); err == nil {
		out = append(out, adobeImageModelDTOs(payload)...)
	} else {
		firstErr = err
	}
	if payload, _, err := a.providerModels(providerLeo); err == nil {
		out = append(out, leoImageModelDTOs(payload)...)
	} else if firstErr == nil {
		firstErr = err
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (a *App) listRemoteVideoModels() ([]VideoModelDTO, error) {
	payload, _, err := a.providerModels(providerLeo)
	if err != nil {
		return nil, err
	}
	return leoModelDTOs(payload), nil
}

func (a *App) generateRemoteImage(prompt, model string, quantity int, aspect string, refs []string) (*service.GenerateResponse, error) {
	cfg, err := a.loadProviderConfig(providerAdobe)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("Adobe API 已禁用")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("Adobe API 尚未配置 API Key")
	}
	if quantity < 1 {
		quantity = 1
	}
	if quantity > 4 {
		quantity = 4
	}
	model = adobeImageVariantForAspect(model, aspect)
	payload := map[string]any{"model": model, "prompt": prompt, "n": quantity, "aspect_ratio": aspect}
	if len(refs) > 0 {
		clean := make([]string, 0, len(refs))
		for _, ref := range refs {
			if value := strings.TrimSpace(ref); value != "" {
				clean = append(clean, value)
			}
		}
		if len(clean) > 0 {
			content := []any{map[string]any{"type": "text", "text": prompt}}
			for _, value := range clean {
				content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": value}})
			}
			payload = map[string]any{
				"model":    model,
				"messages": []any{map[string]any{"role": "user", "content": content}},
			}
			response, err := providerJSON(context.Background(), providerHTTPClient(15*time.Minute), cfg, http.MethodPost, "/v1/chat/completions", payload)
			if err != nil {
				return nil, err
			}
			urls := providerChatImageURLs(response)
			if len(urls) == 0 {
				return nil, errors.New("Adobe2API 未返回参考图生成结果")
			}
			return a.finishRemoteImage(providerAdobe, response, prompt, model, aspect, urls), nil
		}
	}
	response, err := providerJSON(context.Background(), providerHTTPClient(15*time.Minute), cfg, http.MethodPost, "/v1/images/generations", payload)
	if err != nil {
		return nil, err
	}
	urls := providerImageURLs(response)
	if len(urls) == 0 {
		return nil, errors.New("Adobe2API 未返回图片地址")
	}
	return a.finishRemoteImage(providerAdobe, response, prompt, model, aspect, urls), nil
}

func (a *App) generateRemoteLeoImage(prompt, model string, quantity int, aspect string, refs []string) (*service.GenerateResponse, error) {
	cfg, err := a.loadProviderConfig(providerLeo)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("Leo API 已禁用")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("Leo API 尚未配置 API Key")
	}
	model = strings.TrimPrefix(strings.TrimSpace(model), "leo:")
	if model == "" {
		return nil, errors.New("Leo2API 图片模型不能为空")
	}
	if quantity < 1 {
		quantity = 1
	}
	if quantity > 4 {
		quantity = 4
	}
	payload := map[string]any{
		"model":        model,
		"prompt":       prompt,
		"n":            quantity,
		"size":         leoImageSize(aspect),
		"aspect_ratio": aspect,
	}
	cleanRefs := make([]string, 0, len(refs))
	for _, ref := range refs {
		if value := strings.TrimSpace(ref); value != "" {
			cleanRefs = append(cleanRefs, value)
		}
	}
	if len(cleanRefs) > 0 {
		payload["image_urls"] = cleanRefs
	}
	response, err := providerJSON(context.Background(), providerHTTPClient(15*time.Minute), cfg, http.MethodPost, "/v1/images/generations", payload)
	if err != nil {
		return nil, err
	}
	urls := providerImageURLs(response)
	if len(urls) == 0 {
		return nil, errors.New("Leo2API 未返回图片地址")
	}
	return a.finishRemoteImage(providerLeo, response, prompt, model, aspect, urls), nil
}

func (a *App) finishRemoteImage(provider string, response map[string]any, prompt, model, aspect string, urls []string) *service.GenerateResponse {
	genID := providerID(response)
	metadata, _ := json.Marshal(map[string]any{"provider": provider, "model": model})
	_ = a.store.AddProviderGenerationLog(provider, genID, "", "image", string(metadata), 0, model, aspect, prompt, urls, nil, false, "success", "")
	items := make([]service.GenerateDataItem, 0, len(urls))
	for _, value := range urls {
		items = append(items, service.GenerateDataItem{URL: value})
	}
	return &service.GenerateResponse{
		Created: time.Now().Unix(), Data: items,
		Provider: service.GenerateProviderMeta{
			Provider:     provider,
			GenerationID: genID, UsedCookieID: 0, AspectRatio: aspect, ModelID: model,
			SavedFiles: []string{}, AutoSaveEnabled: false,
		},
	}
}

func leoImageSize(aspect string) string {
	switch strings.TrimSpace(aspect) {
	case "16:9":
		return "1536x864"
	case "9:16":
		return "864x1536"
	case "4:3":
		return "1152x864"
	case "3:4":
		return "864x1152"
	default:
		return "1024x1024"
	}
}

func (a *App) generateRemoteVideo(req service.VideoRequest) (*service.VideoResponse, error) {
	cfg, err := a.loadProviderConfig(providerLeo)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("Leo API 已禁用")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("Leo API 尚未配置 API Key")
	}
	model := mapLeoModel(req.ModelSlug)
	if model == "" {
		model = "video-2.0-fast"
	}
	aspect := strings.TrimSpace(req.AspectRatio)
	if aspect == "" {
		aspect = "16:9"
	}
	duration := req.Duration
	if duration <= 0 {
		duration = 8
	}
	payload := map[string]any{
		"model": model, "prompt": req.Prompt, "duration": duration,
		"size": leoSize(model, aspect, req.Resolution), "aspect_ratio": aspect,
		"audio": req.Audio,
	}
	if imageURL := strings.TrimSpace(req.ImageURL); imageURL != "" {
		payload["image_url"] = imageURL
	}
	response, err := providerJSON(context.Background(), providerHTTPClient(20*time.Second), cfg, http.MethodPost, "/v1/video/generations", payload)
	if err != nil {
		return nil, err
	}
	genID := providerID(response)
	pollURL := modelString(response, "poll_url")
	if status := strings.ToLower(modelString(response, "status")); status != "succeeded" && status != "completed" && status != "success" {
		response, err = pollLeoVideo(context.Background(), cfg, genID, pollURL)
		if err != nil {
			return nil, err
		}
	}
	urls := leoVideoURLs(response)
	if len(urls) == 0 {
		return nil, errors.New("Leo2API 未返回视频地址")
	}
	items := make([]service.VideoResponseItem, 0, len(urls))
	for _, value := range urls {
		items = append(items, service.VideoResponseItem{URL: value, MP4URL: value})
	}
	metadata, _ := json.Marshal(map[string]any{"provider": providerLeo, "model": model})
	_ = a.store.AddProviderGenerationLog(providerLeo, genID, "", "video", string(metadata), 0, model, aspect, req.Prompt, urls, nil, false, "success", "")
	return &service.VideoResponse{
		Created: time.Now().Unix(), Data: items,
		Provider: service.VideoResponseProvider{
			GenerationID: genID, CreditCost: 0, CreditCostSource: "remote_api",
			UsedCookieID: 0, Model: model, Resolution: req.Resolution, Duration: duration,
			AspectRatio: aspect, Audio: req.Audio, SavedFiles: []string{}, AutoSaveEnabled: false,
			ClientRequestID: req.ClientRequestID,
		},
	}, nil
}

func providerImageURLs(payload map[string]any) []string {
	items, _ := payload["data"].([]any)
	urls := []string{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		for _, key := range []string{"url", "image_url", "b64_json"} {
			if value := modelString(item, key); value != "" {
				if key == "b64_json" && !strings.HasPrefix(value, "data:") {
					value = "data:image/png;base64," + value
				}
				urls = append(urls, value)
				break
			}
		}
	}
	return urls
}

func providerChatImageURLs(payload map[string]any) []string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	content := strings.TrimSpace(fmt.Sprint(message["content"]))
	if content == "" || content == "<nil>" {
		return nil
	}
	urls := []string{}
	for _, prefix := range []string{"![Generated Image](", "![generated image](", "("} {
		start := strings.Index(content, prefix)
		if start < 0 {
			continue
		}
		start += len(prefix)
		end := strings.IndexAny(content[start:], ")\n \"")
		if end < 0 {
			end = len(content[start:])
		}
		candidate := strings.TrimSpace(content[start : start+end])
		if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			urls = append(urls, candidate)
			break
		}
	}
	return urls
}

func adobeImageVariantForAspect(model, aspect string) string {
	suffix := map[string]string{"1:1": "1x1", "16:9": "16x9", "9:16": "9x16", "4:3": "4x3", "3:4": "3x4"}[strings.TrimSpace(aspect)]
	if suffix == "" {
		suffix = "1x1"
	}
	for _, old := range []string{"1x1", "16x9", "9x16", "4x3", "3x4"} {
		if strings.HasSuffix(model, "-"+old) {
			return strings.TrimSuffix(model, "-"+old) + "-" + suffix
		}
	}
	return model
}
