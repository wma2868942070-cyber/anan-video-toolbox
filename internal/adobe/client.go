package adobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultBaseURL = "http://127.0.0.1:6001"

type Config struct {
	APIKey             string   `json:"api_key"`
	AdminUsername      string   `json:"admin_username"`
	AdminPassword      string   `json:"admin_password"`
	AdminSessionSecret string   `json:"admin_session_secret"`
	PublicBaseURL      string   `json:"public_base_url"`
	Proxy              string   `json:"proxy"`
	UseProxy           bool     `json:"use_proxy"`
	GenerateTimeout    int      `json:"generate_timeout"`
	RefreshInterval    int      `json:"refresh_interval_hours"`
	RetryEnabled       bool     `json:"retry_enabled"`
	RetryMaxAttempts   int      `json:"retry_max_attempts"`
	RetryBackoff       float64  `json:"retry_backoff_seconds"`
	RotationStrategy   string   `json:"token_rotation_strategy"`
	BatchConcurrency   int      `json:"batch_concurrency"`
	RetryStatusCodes   []int    `json:"retry_on_status_codes"`
	RetryErrorTypes    []string `json:"retry_on_error_types"`
}

type Client struct {
	BaseURL  string
	StateDir string
	http     *http.Client
}

func NewClient() *Client {
	return &Client{
		BaseURL:  strings.TrimRight(envOr("ADOBE2API_BASE_URL", DefaultBaseURL), "/"),
		StateDir: StateDir(),
		http:     &http.Client{Timeout: 15 * time.Minute},
	}
}

func StateDir() string {
	if raw := strings.TrimSpace(os.Getenv("ADOBE2API_STATE_DIR")); raw != "" {
		return raw
	}
	// The Windows integration deliberately keeps the Python environment,
	// credentials, logs and generated files in LocalAppData. This fallback is
	// also used when the user opens the desktop EXE/shortcut directly instead
	// of going through scripts/start-windows.ps1.
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "anan-video-toolbox", "adobe2api")
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = "."
	}
	return filepath.Join(base, "anan-video-toolbox", "adobe2api")
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (c *Client) ConfigPath() string {
	return filepath.Join(c.StateDir, "config", "config.json")
}

func (c *Client) LoadConfig() (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(c.ConfigPath())
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	return c.publicJSON(ctx, http.MethodGet, "/api/v1/health", nil)
}

func (c *Client) ServiceJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("读取 Adobe 配置失败: %w", err)
	}
	headers := map[string]string{}
	if strings.TrimSpace(cfg.APIKey) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(cfg.APIKey)
	}
	return c.doJSON(ctx, c.http, method, path, body, headers)
}

func (c *Client) ServiceRaw(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	return c.http.Do(req)
}

func (c *Client) AdminJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	client, err := c.adminHTTPClient(ctx)
	if err != nil {
		return nil, err
	}
	return c.doJSON(ctx, client, method, path, body, nil)
}

func (c *Client) publicJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	return c.doJSON(ctx, c.http, method, path, body, nil)
}

func (c *Client) adminHTTPClient(ctx context.Context) (*http.Client, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("读取 Adobe 后台配置失败: %w", err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 2 * time.Minute, Jar: jar}
	_, err = c.doJSONWithBase(ctx, client, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": cfg.AdminUsername,
		"password": cfg.AdminPassword,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("登录 Adobe 高级后台失败: %w", err)
	}
	return client, nil
}

func (c *Client) doJSON(ctx context.Context, client *http.Client, method, path string, body any, headers map[string]string) (map[string]any, error) {
	return c.doJSONWithBase(ctx, client, method, path, body, headers)
}

func (c *Client) doJSONWithBase(ctx context.Context, client *http.Client, method, path string, body any, headers map[string]string) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := readError(payload)
		if message == "" {
			message = strings.TrimSpace(string(raw))
		}
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("Adobe2API HTTP %d: %s", resp.StatusCode, message)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func readError(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload["detail"].(string); ok {
		return value
	}
	if value, ok := payload["message"].(string); ok {
		return value
	}
	if value, ok := payload["error"].(string); ok {
		return value
	}
	if nested, ok := payload["error"].(map[string]any); ok {
		if value, ok := nested["message"].(string); ok {
			return value
		}
	}
	return ""
}

func (c *Client) GeneratedURL(filename string) (string, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		return "", errors.New("invalid generated filename")
	}
	u, _ := url.Parse(c.BaseURL)
	u.Path = "/generated/" + filename
	u.RawQuery = ""
	return u.String(), nil
}
