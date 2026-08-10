// Package desktop wires the provider APIs into anan视频工具箱. Legacy
// Leonardo-pool methods remain compiled for migration compatibility but are no
// longer exposed by the desktop navigation or used by the creators.
// Methods on App are auto-bound to the JS frontend by Wails so the React UI
// can call them like ordinary async functions.
package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/adobe"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/localminidrama"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/videoclaw"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the root Wails binding. All exported methods are exposed to JS.
type App struct {
	ctx             context.Context
	store           *store.Store
	service         *service.LeonardoPool
	adobe           *adobe.Manager
	videoClaw       *videoclaw.Manager
	localMiniDrama  *localminidrama.Manager
	leonardoBrowser *leonardo.BrowserWorkspaceManager
}

// NewApp constructs the app, opening the SQLite store and bootstrapping defaults.
// It panics on fatal init errors so Wails fails fast at startup.
func NewApp() *App {
	dataDir := defaultDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		panic(fmt.Errorf("desktop: create data dir: %w", err))
	}

	dbPath := filepath.Join(dataDir, "app.db")
	st, err := store.Open(dbPath)
	if err != nil {
		panic(fmt.Errorf("desktop: open store: %w", err))
	}

	// Bootstrap seeds default settings + admin user. We deliberately do not
	// load model_id.txt because the desktop app fetches official models
	// from Leonardo on first run via the auto-sync below.
	if err := st.Bootstrap(""); err != nil {
		panic(fmt.Errorf("desktop: bootstrap store: %w", err))
	}
	// Repair only rows proven to share the same stable Leonardo user id. Never
	// merge by email or browser Cookie because multi-account web sessions can
	// reuse those values while representing different accounts.
	if merged, err := st.MergeDuplicateCookieAccounts(); err != nil {
		log.Printf("desktop: merge duplicate cookie accounts: %v", err)
	} else if merged > 0 {
		log.Printf("desktop: merged %d duplicate cookie account rows", merged)
	}

	client := leonardo.New()
	svc := service.NewLeonardoPool(st, client)

	app := &App{
		store:           st,
		service:         svc,
		adobe:           adobe.NewManager(),
		videoClaw:       videoclaw.NewManager(),
		localMiniDrama:  localminidrama.NewManager(),
		leonardoBrowser: leonardo.NewBrowserWorkspaceManager(dataDir),
	}
	// Provider settings are seeded from the already deployed sidecars when
	// possible. Raw keys are kept in SQLite and never sent to the frontend.
	app.initializeProviderSettings()
	return app
}

// Startup is called by Wails when the app is ready. It captures the runtime
// context which we'll later use for events and window controls.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// The always-on 8001 service owns proactive Leonardo refresh. Keeping a
	// second scheduler in the desktop process previously caused duplicate
	// get-session calls. Manual desktop refreshes remain safe through the shared
	// SQLite refresh lease.
}

// Shutdown closes the database when the window is closed.
func (a *App) Shutdown(_ context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
}

// ----- Health / smoke test -------------------------------------------------

// Ping is a quick smoke test from the frontend to verify bindings work.
func (a *App) Ping() string {
	return "anan视频工具箱 desktop bindings ok"
}

type LeonardoServiceStatus struct {
	Running       bool   `json:"running"`
	BaseURL       string `json:"baseURL"`
	AdminURL      string `json:"adminURL"`
	Total         int    `json:"total"`
	Ready         int    `json:"ready"`
	Cooling       int    `json:"cooling"`
	Disabled      int    `json:"disabled"`
	TotalBalance  int64  `json:"totalBalance"`
	ActiveTasks   int    `json:"activeTasks"`
	LeonardoTasks int    `json:"leonardoTasks"`
	Error         string `json:"error"`
}

// LeonardoServiceStatus reports the always-on 8001 gateway without exposing
// its API key or account identities. An offline gateway is returned as state
// rather than an error so the local account page remains usable.
func (a *App) LeonardoServiceStatus() LeonardoServiceStatus {
	const baseURL = "http://127.0.0.1:8001"
	out := LeonardoServiceStatus{BaseURL: baseURL, AdminURL: baseURL + "/admin"}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/admin/status")
	if err != nil {
		out.Error = "8001 本地服务未运行"
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out.Error = fmt.Sprintf("8001 本地服务返回 HTTP %d", resp.StatusCode)
		return out
	}
	var status struct {
		Status        string `json:"status"`
		Total         int    `json:"total"`
		Ready         int    `json:"ready"`
		Cooling       int    `json:"cooling"`
		Disabled      int    `json:"disabled"`
		TotalBalance  int64  `json:"total_balance"`
		ActiveTasks   int    `json:"active_tasks"`
		LeonardoTasks int    `json:"leonardo_tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		out.Error = "8001 本地服务状态响应无效"
		return out
	}
	out.Running = strings.EqualFold(status.Status, "ok")
	out.Total = status.Total
	out.Ready = status.Ready
	out.Cooling = status.Cooling
	out.Disabled = status.Disabled
	out.TotalBalance = status.TotalBalance
	out.ActiveTasks = status.ActiveTasks
	out.LeonardoTasks = status.LeonardoTasks
	return out
}

func (a *App) RestartLeonardoService() (LeonardoServiceStatus, error) {
	status := a.LeonardoServiceStatus()
	if status.ActiveTasks > 0 {
		return status, fmt.Errorf("当前有 %d 个生成任务仍在 8001 中运行，请等待完成后再重启", status.ActiveTasks)
	}
	if err := restartLeonardoGateway(); err != nil {
		return status, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		status = a.LeonardoServiceStatus()
		if status.Running {
			return status, nil
		}
	}
	return status, errors.New("8001 本地服务重启后健康检查超时")
}

// AppInfoDTO carries metadata shown in the About dialog.
type AppInfoDTO struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Author     string `json:"author"`
	Repository string `json:"repository"`
	License    string `json:"license"`
}

// AppInfo returns static metadata about the desktop build.
func (a *App) AppInfo() AppInfoDTO {
	return AppInfoDTO{
		Name:       "anan视频工具箱",
		Version:    "1.0.0",
		Author:     "wma2868942070-cyber",
		Repository: "https://github.com/wma2868942070-cyber/anan-video-toolbox",
		License:    "PolyForm Noncommercial 1.0.0",
	}
}

// OpenURL opens an arbitrary URL in the user's default browser.
func (a *App) OpenURL(url string) error {
	if a.ctx == nil {
		return fmt.Errorf("desktop: app not ready")
	}
	wailsruntime.BrowserOpenURL(a.ctx, url)
	return nil
}

// ----- VideoClaw AI director ----------------------------------------------

func (a *App) VideoClawServiceStatus() videoclaw.ServiceStatus {
	return a.videoClaw.Status(context.Background())
}

func (a *App) StartVideoClawService() (videoclaw.ServiceStatus, error) {
	return a.videoClaw.Start(context.Background())
}

func (a *App) RestartVideoClawService() (videoclaw.ServiceStatus, error) {
	return a.videoClaw.Restart(context.Background())
}

// ----- LocalMiniDrama ------------------------------------------------------

func (a *App) LocalMiniDramaServiceStatus() localminidrama.ServiceStatus {
	return a.localMiniDrama.Status(context.Background())
}

func (a *App) StartLocalMiniDramaService() (localminidrama.ServiceStatus, error) {
	return a.localMiniDrama.Start(context.Background())
}

func (a *App) RestartLocalMiniDramaService() (localminidrama.ServiceStatus, error) {
	return a.localMiniDrama.Restart(context.Background())
}

func (a *App) SyncLocalMiniDramaModels() (map[string]any, error) {
	return a.localMiniDrama.SyncModels(context.Background())
}

// ----- Cookie pool ---------------------------------------------------------

// CookieDTO is a JSON-friendly view of a stored cookie. We avoid exposing the
// raw cookie value to the frontend (security + payload size), surfacing only
// the metadata operators need.
type CookieDTO struct {
	ID              int64  `json:"id"`
	AccountID       string `json:"account_id"`
	Email           string `json:"email"`
	AutoRecoverable bool   `json:"auto_recoverable"`
	IsActive        bool   `json:"is_active"`
	SessionStatus   string `json:"session_status"`
	JWTExpiresAt    int64  `json:"jwt_expires_at"`
	RefreshFails    int    `json:"refresh_fail_count"`
	RefreshReason   string `json:"refresh_fail_reason"`
	ErrorUntil      int64  `json:"error_until"`
	LastRefreshAt   int64  `json:"last_refresh_at"`
	LastBalance     int64  `json:"last_balance"`
	LastError       string `json:"last_error"`
	LastUsedAt      int64  `json:"last_used_at"`
	LastCheckedAt   int64  `json:"last_checked_at"`
	DisabledReason  string `json:"disabled_reason"`
	DisabledAt      int64  `json:"disabled_at"`
	CreatedAt       int64  `json:"created_at"`
	Status          string `json:"status"` // READY | TEMPORARY | INVALID | ABNORMAL | DEPLETED | DISABLED
}

func cookieToDTO(c store.Cookie) CookieDTO {
	status := "DISABLED"
	if c.IsActive == 1 {
		switch strings.ToLower(strings.TrimSpace(c.SessionStatus)) {
		case "temporary_unavailable":
			status = "TEMPORARY"
		case "invalid":
			status = "INVALID"
		case "abnormal":
			status = "ABNORMAL"
		case "":
			if c.LastBalance > 0 {
				status = "READY"
			} else {
				status = "DEPLETED"
			}
		default:
			if c.LastBalance > 0 {
				status = "READY"
			} else {
				status = "DEPLETED"
			}
		}
	} else {
		switch strings.ToLower(strings.TrimSpace(c.SessionStatus)) {
		case "invalid":
			status = "INVALID"
		case "abnormal":
			status = "ABNORMAL"
		}
	}
	return CookieDTO{
		ID:              c.ID,
		AccountID:       c.AccountID,
		Email:           c.Email,
		AutoRecoverable: authValueHasSessionCookie(c.Value),
		IsActive:        c.IsActive == 1,
		SessionStatus:   c.SessionStatus,
		JWTExpiresAt:    c.JWTExpiresAt,
		RefreshFails:    c.RefreshFails,
		RefreshReason:   c.RefreshReason,
		ErrorUntil:      c.ErrorUntil,
		LastRefreshAt:   c.LastRefreshAt,
		LastBalance:     c.LastBalance,
		LastError:       c.LastError,
		LastUsedAt:      c.LastUsedAt,
		LastCheckedAt:   c.LastCheckedAt,
		DisabledReason:  c.DisabledReason,
		DisabledAt:      c.DisabledAt,
		CreatedAt:       c.CreatedAt,
		Status:          status,
	}
}

func authValueHasSessionCookie(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "cookie=") || strings.HasPrefix(lower, "cookie:") {
			return strings.TrimSpace(line[7:]) != ""
		}
	}
	return strings.Contains(value, ";") && strings.Contains(value, "=")
}

// ListCookies returns every cookie row, newest first.
func (a *App) ListCookies() ([]CookieDTO, error) {
	rows, err := a.store.ListCookies()
	if err != nil {
		return nil, err
	}
	out := make([]CookieDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, cookieToDTO(c))
	}
	return out, nil
}

// AddCookieResult is what the frontend gets after a validated add.
type AddCookieResult struct {
	Email            string `json:"email"`
	Balance          int64  `json:"balance"`
	UpdatedExisting  bool   `json:"updated_existing"`
	MergedDuplicates int    `json:"merged_duplicates"`
	Warning          string `json:"warning,omitempty"`
}

type DeleteCookieResult struct {
	WorkspacesRemoved int `json:"workspaces_removed"`
}

// AddCookie validates the raw auth payload (cookie string + optional token=)
// against Leonardo, persists it on success, and returns email + balance.
func (a *App) AddCookie(rawAuthValue string) (*AddCookieResult, error) {
	info, err := a.service.AddCookieValidated(rawAuthValue)
	if err != nil {
		return nil, err
	}
	return &AddCookieResult{
		Email:            info.Email,
		Balance:          info.Balance,
		UpdatedExisting:  info.UpdatedExisting,
		MergedDuplicates: info.MergedDuplicates,
	}, nil
}

// UpdateCookie replaces an existing cookie's payload with a freshly pasted
// one, validating against Leonardo first. Returns the new email/balance.
func (a *App) UpdateCookie(id int64, rawAuthValue string) (*AddCookieResult, error) {
	info, err := a.service.UpdateCookieValidated(id, rawAuthValue)
	if err != nil {
		return nil, err
	}
	a.emitCookiesChanged()
	return &AddCookieResult{Email: info.Email, Balance: info.Balance}, nil
}

// DeleteCookie removes the pool row and its stable-account recovery workspace.
// Association is based only on Leonardo's stable account id, never email or
// Cookie text, so deleting one account cannot remove another account's Chrome.
func (a *App) DeleteCookie(id int64) (*DeleteCookieResult, error) {
	cookie, err := a.store.GetCookieByID(id)
	if err != nil {
		return nil, err
	}
	if cookie == nil {
		return &DeleteCookieResult{}, nil
	}
	if err := a.store.DeleteCookie(id); err != nil {
		return nil, err
	}
	removed, syncErr := a.leonardoBrowser.RemoveByAccountID(cookie.AccountID)
	a.emitCookiesChanged()
	if syncErr != nil {
		return nil, fmt.Errorf("账号已删除，但同步移除隔离工作区失败: %w", syncErr)
	}
	return &DeleteCookieResult{WorkspacesRemoved: removed}, nil
}

// ToggleCookie enables or disables a cookie without deleting it.
func (a *App) ToggleCookie(id int64, enabled bool) error {
	return a.store.ToggleCookie(id, enabled)
}

// CookieRefreshResult summarises a bulk profile/session refresh run.
type CookieRefreshResult struct {
	Checked   int `json:"checked"`
	OK        int `json:"ok"`
	Reenabled int `json:"reenabled"`
	Merged    int `json:"merged"`
}

// RefreshCookieProfiles re-fetches balance + email for every cookie. Disabled
// cookies are not auto-disabled further; depleted ones get marked DEPLETED.
func (a *App) RefreshCookieProfiles() (*CookieRefreshResult, error) {
	res, err := a.service.RefreshCookieProfiles()
	if err != nil {
		return nil, err
	}
	return &CookieRefreshResult{Checked: res.Checked, OK: res.OK, Reenabled: res.Reenabled, Merged: res.Merged}, nil
}

// RefreshCookieSessions re-resolves the JWT for every cookie via TLS impersonation.
func (a *App) RefreshCookieSessions() (*CookieRefreshResult, error) {
	res, err := a.service.RefreshCookieSessions()
	if err != nil {
		return nil, err
	}
	return &CookieRefreshResult{Checked: res.Checked, OK: res.OK, Reenabled: res.Reenabled, Merged: res.Merged}, nil
}

func (a *App) ListLeonardoBrowserWorkspaces() ([]leonardo.BrowserWorkspace, error) {
	items, err := a.leonardoBrowser.List()
	if err != nil {
		return nil, err
	}
	rows, err := a.store.ListCookies()
	if err != nil {
		return nil, err
	}
	valid := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if accountID := strings.TrimSpace(row.AccountID); accountID != "" {
			valid[accountID] = struct{}{}
		}
	}
	changed := false
	for _, item := range items {
		accountID := strings.TrimSpace(item.AccountID)
		if accountID == "" {
			continue
		}
		if _, ok := valid[accountID]; ok {
			continue
		}
		if _, err := a.leonardoBrowser.RemoveByAccountID(accountID); err != nil {
			return nil, err
		}
		changed = true
	}
	if changed {
		items, err = a.leonardoBrowser.List()
		if err != nil {
			return nil, err
		}
	}
	// Accounts imported through cURL do not have a managed Chrome profile yet.
	// Materialize a bound, unopened workspace so the account pool and isolated
	// pool stay one-to-one without silently launching browsers.
	for _, row := range rows {
		accountID := strings.TrimSpace(row.AccountID)
		if accountID == "" {
			continue
		}
		found := false
		for _, item := range items {
			if strings.TrimSpace(item.AccountID) == accountID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if _, err := a.leonardoBrowser.EnsureBoundWorkspace(accountID, fmt.Sprintf("账号池-%d", row.ID)); err != nil {
			return nil, err
		}
		items, err = a.leonardoBrowser.List()
		if err != nil {
			return nil, err
		}
	}
	return decorateLeonardoWorkspaceNames(items, rows), nil
}

// decorateLeonardoWorkspaceNames keeps the opaque workspace id and profile
// directory unchanged while making the account cards distinguishable in the
// UI. The email is read from the current cookie-pool row, so it follows an
// account update and is not duplicated into a second source of truth.
func decorateLeonardoWorkspaceNames(items []leonardo.BrowserWorkspace, rows []store.Cookie) []leonardo.BrowserWorkspace {
	emails := make(map[string]string, len(rows))
	for _, row := range rows {
		accountID := strings.TrimSpace(row.AccountID)
		email := strings.TrimSpace(row.Email)
		if accountID != "" && email != "" {
			emails[accountID] = email
		}
	}
	for i := range items {
		email := emails[strings.TrimSpace(items[i].AccountID)]
		if email == "" {
			continue
		}
		suffix := "（" + email + "）"
		if !strings.HasSuffix(strings.TrimSpace(items[i].Name), suffix) {
			items[i].Name = strings.TrimSpace(items[i].Name) + suffix
		}
	}
	return items
}

func (a *App) LaunchLeonardoBrowserWorkspace(name string) (leonardo.BrowserWorkspace, error) {
	return a.leonardoBrowser.Launch(name)
}

func (a *App) ReopenLeonardoBrowserWorkspace(workspaceID string) (leonardo.BrowserWorkspace, error) {
	return a.leonardoBrowser.Reopen(workspaceID)
}

func (a *App) ImportLeonardoBrowserWorkspace(workspaceID string) (*AddCookieResult, error) {
	workspace, err := a.leonardoBrowser.Get(workspaceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cookie, err := a.leonardoBrowser.ReadCookie(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	info, err := importLeonardoWorkspaceCookie(
		workspace,
		"cookie="+cookie,
		a.service.AddCookieValidated,
		a.service.RefreshBoundCookieValidated,
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspace.AccountID) == "" {
		if err := a.leonardoBrowser.BindAccount(workspaceID, info.AccountID); err != nil {
			// AddCookieValidated has already refreshed the existing pool row. A
			// second Chrome profile for the same stable account must not become a
			// second recovery source, but this is not a Cookie-read failure.
			if errors.Is(err, leonardo.ErrAccountWorkspaceAlreadyBound) {
				return &AddCookieResult{
					Email:            info.Email,
					Balance:          info.Balance,
					UpdatedExisting:  true,
					MergedDuplicates: info.MergedDuplicates,
					Warning:          err.Error(),
				}, nil
			}
			return nil, err
		}
	}
	a.emitCookiesChanged()
	return &AddCookieResult{
		Email:            info.Email,
		Balance:          info.Balance,
		UpdatedExisting:  info.UpdatedExisting,
		MergedDuplicates: info.MergedDuplicates,
	}, nil
}

func importLeonardoWorkspaceCookie(
	workspace leonardo.BrowserWorkspace,
	rawAuthValue string,
	add func(string) (service.UserInfoResult, error),
	refreshBound func(string, string) (service.UserInfoResult, error),
) (service.UserInfoResult, error) {
	if boundAccountID := strings.TrimSpace(workspace.AccountID); boundAccountID != "" {
		// A persistent Chrome profile has one immutable owner. Validate the
		// current browser identity before touching the pool so switching logins
		// cannot create or overwrite another account row.
		return refreshBound(boundAccountID, rawAuthValue)
	}
	return add(rawAuthValue)
}

// CookieHealth aggregates status counts for the dashboard hero cards.
type CookieHealth struct {
	Total         int   `json:"total"`
	Ready         int   `json:"ready"`
	Temporary     int   `json:"temporary"`
	Depleted      int   `json:"depleted"`
	Disabled      int   `json:"disabled"`
	TotalBalance  int64 `json:"total_balance"`
	ActiveBalance int64 `json:"active_balance"`
}

// CookieHealth returns aggregated counts for the dashboard hero cards.
func (a *App) CookieHealth() (*CookieHealth, error) {
	rows, err := a.store.ListCookies()
	if err != nil {
		return nil, err
	}
	out := CookieHealth{Total: len(rows)}
	for _, c := range rows {
		out.TotalBalance += c.LastBalance
		switch {
		case c.IsActive != 1:
			out.Disabled++
		case strings.EqualFold(c.SessionStatus, "temporary_unavailable"):
			out.Temporary++
		case c.LastBalance > 0:
			out.Ready++
			out.ActiveBalance += c.LastBalance
		default:
			out.Depleted++
		}
	}
	return &out, nil
}

// ----- Settings ------------------------------------------------------------

// GetSetting returns a stored value or the provided fallback.
func (a *App) GetSetting(key, fallback string) (string, error) {
	return a.store.GetSetting(key, fallback)
}

// SetSetting writes a value (creates the key when missing).
func (a *App) SetSetting(key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("设置项名称不能为空")
	}
	return a.store.SetSetting(key, value)
}

// ----- Adobe Firefly / adobe2api ------------------------------------------

func (a *App) AdobeServiceStatus() adobe.ServiceStatus {
	return a.adobe.Status(context.Background())
}

func (a *App) StartAdobeService() (adobe.ServiceStatus, error) {
	return a.adobe.Start(context.Background())
}

func (a *App) RestartAdobeService() (adobe.ServiceStatus, error) {
	return a.adobe.Restart(context.Background())
}

func (a *App) ListAdobeProfiles() (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodGet, "/api/v1/refresh-profiles", nil)
}

func (a *App) ListAdobeTokens() (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodGet, "/api/v1/tokens", nil)
}

func (a *App) GetAdobeConfig() (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodGet, "/api/v1/config", nil)
}

func (a *App) UpdateAdobeConfig(values map[string]any) (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodPut, "/api/v1/config", values)
}

func (a *App) ImportAdobeCookie(name, rawCookie string) (map[string]any, error) {
	rawCookie = strings.TrimSpace(rawCookie)
	if rawCookie == "" {
		return nil, fmt.Errorf("Cookie 不能为空")
	}
	var cookie any = rawCookie
	var parsed any
	if json.Unmarshal([]byte(rawCookie), &parsed) == nil {
		cookie = parsed
	}
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodPost, "/api/v1/refresh-profiles/import-cookie", map[string]any{
		"name": strings.TrimSpace(name), "cookie": cookie,
	})
}

func (a *App) ImportAdobeCookieFiles() (map[string]any, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("desktop: app not ready")
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   "选择 Adobe Cookie JSON 文件",
		Filters: []wailsruntime.FileFilter{{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"}},
	})
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), readErr)
		}
		var cookie any
		if err := json.Unmarshal(raw, &cookie); err != nil {
			cookie = strings.TrimSpace(string(raw))
		}
		items = append(items, map[string]any{"name": strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "cookie": cookie})
	}
	if len(items) == 0 {
		return map[string]any{"status": "cancelled", "imported": []any{}}, nil
	}
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodPost, "/api/v1/refresh-profiles/import-cookie-batch", map[string]any{"items": items})
}

func (a *App) RefreshAdobeProfile(profileID string) (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodPost, "/api/v1/refresh-profiles/"+profileID+"/refresh-now", nil)
}

func (a *App) ToggleAdobeProfile(profileID string, enabled bool) (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodPut, "/api/v1/refresh-profiles/"+profileID+"/enabled", map[string]any{"enabled": enabled})
}

func (a *App) DeleteAdobeProfile(profileID string) (map[string]any, error) {
	return a.adobe.Client.AdminJSON(context.Background(), http.MethodDelete, "/api/v1/refresh-profiles/"+profileID, nil)
}

func (a *App) RefreshAllAdobeProfiles() (map[string]any, error) {
	payload, err := a.ListAdobeProfiles()
	if err != nil {
		return nil, err
	}
	profiles, _ := payload["profiles"].([]any)
	refreshed := 0
	failed := 0
	errorsOut := []string{}
	for _, raw := range profiles {
		profile, _ := raw.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(profile["id"]))
		if id == "" {
			continue
		}
		if _, err := a.RefreshAdobeProfile(id); err != nil {
			failed++
			errorsOut = append(errorsOut, err.Error())
		} else {
			refreshed++
		}
	}
	return map[string]any{"refreshed": refreshed, "failed": failed, "errors": errorsOut}, nil
}

func (a *App) ListAdobeBrowserWorkspaces() ([]adobe.BrowserWorkspace, error) {
	return a.adobe.ListWorkspaces()
}

func (a *App) LaunchAdobeBrowserWorkspace(name string) (adobe.BrowserWorkspace, error) {
	return a.adobe.LaunchWorkspace(name)
}

func (a *App) ImportAdobeBrowserWorkspace(workspaceID string) (map[string]any, error) {
	return a.adobe.ImportWorkspaceCookies(context.Background(), workspaceID)
}

func (a *App) AdobeAdminPassword() (string, error) {
	cfg, err := a.adobe.Client.LoadConfig()
	if err != nil {
		return "", err
	}
	return cfg.AdminPassword, nil
}

func (a *App) ResetAdobeAdminPassword() (string, error) {
	return a.adobe.ResetAdminPassword(context.Background())
}

func (a *App) SyncAdobeModels() (map[string]any, error) {
	return a.adobe.Client.ServiceJSON(context.Background(), http.MethodGet, "/v1/models", nil)
}

// ----- Image generation ----------------------------------------------------

// ImageGenerateRequest is the JSON-friendly request from the UI.
type ImageGenerateRequest struct {
	Prompt             string   `json:"prompt"`
	ModelID            string   `json:"modelId"`
	N                  int      `json:"n"`
	AspectRatio        string   `json:"aspectRatio"`
	ReferenceImageURLs []string `json:"referenceImageURLs"`
	ReferenceImageIDs  []string `json:"referenceImageIds"` // pre-uploaded ids
}

// GenerateImage delegates to the configured Adobe API. The frontend gets a
// normalized response so it can render provider URLs and metadata.
func (a *App) GenerateImage(req ImageGenerateRequest) (*service.GenerateResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	modelID := strings.TrimSpace(req.ModelID)
	if modelID == "" {
		models, err := a.listRemoteImageModels()
		if err != nil {
			return nil, err
		}
		if len(models) > 0 {
			modelID = models[0].ModelID
		}
	}
	if modelID == "" {
		return nil, fmt.Errorf("Adobe2API 尚未返回可用图片模型")
	}
	aspect := strings.TrimSpace(req.AspectRatio)
	if aspect == "" {
		aspect, _ = a.store.GetSetting("default_aspect_ratio", "1:1")
	}
	n := req.N
	if n <= 0 {
		n = 1
	}
	if strings.HasPrefix(strings.ToLower(modelID), "leo:") {
		log.Printf("[generate.image] provider=leo model=%s aspect=%s n=%d refs=%d",
			modelID, aspect, n, len(req.ReferenceImageURLs))
		res, err := a.generateRemoteLeoImage(req.Prompt, modelID, n, aspect, req.ReferenceImageURLs)
		if err != nil {
			log.Printf("[generate.image] error: %v", err)
			return nil, err
		}
		log.Printf("[generate.image] success: provider=leo gen=%s urls=%d", res.Provider.GenerationID, len(res.Data))
		return res, nil
	}
	log.Printf("[generate.image] provider=adobe model=%s aspect=%s n=%d urls=%d ids=%d",
		modelID, aspect, n, len(req.ReferenceImageURLs), len(req.ReferenceImageIDs))
	res, err := a.generateRemoteImage(req.Prompt, modelID, n, aspect, req.ReferenceImageURLs)
	if err != nil {
		log.Printf("[generate.image] error: %v", err)
		return nil, err
	}
	log.Printf("[generate.image] success: gen=%s urls=%d", res.Provider.GenerationID, len(res.Data))
	return res, nil
}

// ----- Video generation ----------------------------------------------------

// VideoGenerateRequest mirrors service.VideoRequest with JSON-friendly tags.
type VideoGenerateRequest struct {
	Prompt      string `json:"prompt"`
	ModelSlug   string `json:"modelSlug"`
	AspectRatio string `json:"aspectRatio"`
	Resolution  string `json:"resolution"`
	Duration    int    `json:"duration"`
	Audio       bool   `json:"audio"`
	ImageURL    string `json:"imageURL"`
	ImageID     string `json:"imageId"` // pre-uploaded init image id
}

// GenerateVideo runs the configured Leo2API video pipeline.
func (a *App) GenerateVideo(req VideoGenerateRequest) (*service.VideoResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(req.ImageID) != "" {
		return nil, fmt.Errorf("当前已切换到 Leo2API，参考图请使用可访问的图片 URL")
	}
	model := mapLeoModel(req.ModelSlug)
	if model == "" {
		model = "video-2.0-fast"
	}
	res, err := a.generateRemoteVideo(service.VideoRequest{
		Prompt:      req.Prompt,
		ModelSlug:   model,
		AspectRatio: req.AspectRatio,
		Resolution:  req.Resolution,
		Duration:    req.Duration,
		Audio:       req.Audio,
		ImageURL:    req.ImageURL,
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// emitCookiesChanged signals the frontend that cookie balance/state changed
// so it can refetch lists without polling. Safe no-op when ctx is nil
// (e.g. pre-startup or in tests).
func (a *App) emitCookiesChanged() {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "cookies:changed")
}

// ----- Filesystem dialogs --------------------------------------------------

// OpenDirectoryDialog shows the OS-native folder picker. Returns "" on cancel.
func (a *App) OpenDirectoryDialog(currentPath string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("desktop: app not ready")
	}
	opts := wailsruntime.OpenDialogOptions{
		Title: "选择文件夹",
	}
	if currentPath != "" {
		opts.DefaultDirectory = currentPath
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, opts)
}

// OpenInFileManager opens the given path in the OS file manager.
// Uses xdg-open / open / explorer depending on platform via Wails browser pkg.
func (a *App) OpenInFileManager(path string) error {
	abs := strings.TrimSpace(path)
	if abs == "" {
		return fmt.Errorf("path is required")
	}
	// Resolve relative paths against the data dir so settings like
	// "data/generated" still locate the right folder.
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(defaultDataDir(), abs)
	}
	if _, err := os.Stat(abs); err != nil {
		// Auto-create the directory before opening so brand-new save targets
		// don't 404 the file manager call.
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
				return mkErr
			}
		} else {
			return err
		}
	}
	wailsruntime.BrowserOpenURL(a.ctx, "file://"+abs)
	return nil
}

// DownloadAsset downloads a remote URL to a user-chosen location via the
// native save dialog. Returns the absolute path written, or "" if the user
// cancelled. Used by the Lightbox preview download button.
func (a *App) DownloadAsset(url string, suggestedName string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("desktop: app not ready")
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	suggested := strings.TrimSpace(suggestedName)
	if suggested == "" {
		suggested = filepath.Base(stripQueryString(url))
	}
	if suggested == "" || suggested == "/" {
		suggested = "leonardo-asset"
	}

	// Default to ~/Downloads when available, fall back to user config dir.
	defaultDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		downloads := filepath.Join(home, "Downloads")
		if _, statErr := os.Stat(downloads); statErr == nil {
			defaultDir = downloads
		} else {
			defaultDir = home
		}
	}

	target, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:                "保存生成结果",
		DefaultDirectory:     defaultDir,
		DefaultFilename:      suggested,
		CanCreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if target == "" {
		// User cancelled — return empty string, no error.
		return "", nil
	}

	body, _, err := a.service.Client().Download(url)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(target, body, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// stripQueryString removes any ?query suffix so filename derivation works
// against URLs that include cache busters.
func stripQueryString(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// UploadLocalImage forwards a raw image (drag-drop / file picker) to
// Leonardo via the cookie pool and returns the init image id.
//
// We accept two positional string args because Wails marshals positional
// arguments more reliably than struct payloads across the JS↔Go bridge.
func (a *App) UploadLocalImage(base64Payload, extension string) (string, error) {
	raw := strings.TrimSpace(base64Payload)
	if raw == "" {
		return "", fmt.Errorf("empty image payload")
	}
	// Strip data URL prefix (e.g. "data:image/png;base64,") if present so
	// the frontend can pass either form.
	if i := strings.Index(raw, ","); i >= 0 && strings.HasPrefix(raw, "data:") {
		raw = raw[i+1:]
	}
	bytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	ext := strings.ToLower(strings.TrimSpace(extension))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		ext = "jpg"
	}
	log.Printf("[upload] received: bytes=%d ext=%s", len(bytes), ext)
	id, err := a.service.UploadLocalImage(bytes, ext)
	if err != nil {
		log.Printf("[upload] failed: %v", err)
		return "", err
	}
	log.Printf("[upload] success: id=%s", id)
	return id, nil
}

// ----- Models --------------------------------------------------------------

// ModelDTO is the JSON-friendly row from the models table.
type ModelDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ModelID   string `json:"modelId"`
	SDVersion string `json:"sdVersion"`
	IsDefault bool   `json:"isDefault"`
	CreatedAt int64  `json:"createdAt"`
}

// SyncImageModels pulls the official Leonardo catalog and upserts it locally.
// Used by the Models page "Sync" button.
func (a *App) SyncImageModels() (*service.ModelSyncResult, error) {
	return nil, fmt.Errorf("模型管理已移除，请在接口管理中测试 Adobe API")
}

// ListImageModels returns all image models from the local DB.
func (a *App) ListImageModels() ([]ModelDTO, error) {
	return a.listRemoteImageModels()
}

// AddImageModel inserts a new image model entry.
func (a *App) AddImageModel(name, modelID string) error {
	return fmt.Errorf("模型管理已移除，请直接在接口管理中检测模型")
}

// DeleteImageModel removes a model row by id.
func (a *App) DeleteImageModel(id int64) error {
	return fmt.Errorf("模型管理已移除")
}

// SetDefaultImageModel promotes a row to default.
func (a *App) SetDefaultImageModel(id int64) error {
	return fmt.Errorf("模型管理已移除")
}

// VideoModelDTO is the catalog entry shape exposed to the UI.
type VideoModelDTO struct {
	Name                   string   `json:"name"`
	Family                 string   `json:"family"`
	Slug                   string   `json:"slug"`
	ModelValue             string   `json:"modelValue"`
	RequestProfile         string   `json:"requestProfile"`
	DefaultMode            string   `json:"defaultMode"`
	SupportedModes         []string `json:"supportedModes"`
	DurationOptions        []int    `json:"durationOptions"`
	DefaultDuration        int      `json:"defaultDuration"`
	SupportsAudio          bool     `json:"supportsAudio"`
	AudioPolicy            string   `json:"audioPolicy"`
	SupportsRefImage       bool     `json:"supportsRefImage"`
	RequiresRefImage       bool     `json:"requiresRefImage"`
	SupportsEndFrame       bool     `json:"supportsEndFrame"`
	SupportsImageReference bool     `json:"supportsImageReference"`
	SupportsVideoReference bool     `json:"supportsVideoReference"`
	SupportsAudioReference bool     `json:"supportsAudioReference"`
	DefaultAspect          string   `json:"defaultAspect"`
	DocsURL                string   `json:"docsURL"`
	Notes                  string   `json:"notes"`
}

// ListVideoModels returns the static video model catalog.
func (a *App) ListVideoModels() ([]VideoModelDTO, error) {
	return a.listRemoteVideoModels()
}

// ----- Library / generation logs ------------------------------------------

// GenerationLogDTO is a JSON-friendly row from generation_logs.
type GenerationLogDTO struct {
	ID                   int64    `json:"id"`
	Provider             string   `json:"provider"`
	ProviderGenerationID string   `json:"providerGenerationID"`
	ProviderAccountID    string   `json:"providerAccountID"`
	MediaType            string   `json:"mediaType"`
	MetadataJSON         string   `json:"metadataJSON"`
	UsedCookieID         int64    `json:"usedCookieID"`
	ModelID              string   `json:"modelID"`
	AspectRatio          string   `json:"aspectRatio"`
	Prompt               string   `json:"prompt"`
	ImageURLs            []string `json:"imageURLs"`
	SavedFiles           []string `json:"savedFiles"`
	SaveEnabled          bool     `json:"saveEnabled"`
	Status               string   `json:"status"`
	ErrorMessage         string   `json:"errorMessage"`
	CreatedAt            int64    `json:"createdAt"`
}

// SyncLeonardoLibrary imports recent web generations from every account in
// the account pool, then the frontend reloads the local material list.
func (a *App) SyncLeonardoLibrary(limit int) (*service.LibrarySyncResult, error) {
	return a.service.SyncLibrary(limit)
}

// DeleteGenerationLog hides one item from the local material library. It does
// not delete the original image/video from the user's Leonardo account.
func (a *App) DeleteGenerationLog(id int64) error {
	return a.store.HideGenerationLog(id)
}

// SaveGenerationResult reports a material-card batch save.
type SaveGenerationResult struct {
	Files  []string `json:"files"`
	Failed int      `json:"failed"`
}

// SaveGenerationLog lets the user choose one folder, then downloads every
// asset attached to that material card and remembers the saved paths.
func (a *App) SaveGenerationLog(id int64) (*SaveGenerationResult, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("desktop: app not ready")
	}
	row, err := a.store.GetGenerationLog(id)
	if err != nil {
		return nil, err
	}
	urls := []string{}
	if err := json.Unmarshal([]byte(row.ImageURLsJSON), &urls); err != nil {
		return nil, fmt.Errorf("素材链接解析失败: %w", err)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("这个素材没有可保存的文件")
	}

	defaultDir, _ := a.store.GetSetting("save_images_dir", "")
	if !filepath.IsAbs(defaultDir) {
		defaultDir = ""
	}
	if defaultDir == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			defaultDir = filepath.Join(home, "Downloads")
			if _, statErr := os.Stat(defaultDir); statErr != nil {
				defaultDir = home
			}
		}
	}
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "选择素材保存文件夹",
		DefaultDirectory: defaultDir,
	})
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return &SaveGenerationResult{Files: []string{}}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	_ = a.store.SetSetting("save_images_dir", dir)

	result := &SaveGenerationResult{Files: []string{}}
	baseID := safeFilePart(row.ProviderGenerationID)
	if baseID == "" {
		baseID = fmt.Sprintf("material-%d", row.ID)
	}
	for index, rawURL := range urls {
		body, contentType, downloadErr := a.service.Client().Download(rawURL)
		if downloadErr != nil {
			result.Failed++
			continue
		}
		ext := assetExtension(rawURL, contentType)
		provider := strings.TrimSpace(row.Provider)
		if provider == "" {
			provider = "leonardo"
		}
		name := fmt.Sprintf("%s-%s-%02d%s", safeFilePart(provider), baseID, index+1, ext)
		target := uniqueFilePath(filepath.Join(dir, name))
		if writeErr := os.WriteFile(target, body, 0o644); writeErr != nil {
			result.Failed++
			continue
		}
		result.Files = append(result.Files, target)
	}
	if len(result.Files) == 0 {
		return nil, fmt.Errorf("素材保存失败，请检查网络和目标文件夹")
	}
	if err := a.store.SetGenerationSavedFiles(id, result.Files); err != nil {
		return nil, err
	}
	return result, nil
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 36 {
		value = value[:36]
	}
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func assetExtension(rawURL, contentType string) string {
	ext := strings.ToLower(filepath.Ext(stripQueryString(rawURL)))
	if len(ext) >= 2 && len(ext) <= 6 {
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])) {
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func uniqueFilePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}

// ListGenerationLogs returns the most recent generations (capped at 200).
func (a *App) ListGenerationLogs(limit int) ([]GenerationLogDTO, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := a.store.ListGenerationLogs(limit)
	if err != nil {
		return nil, err
	}
	out := make([]GenerationLogDTO, 0, len(rows))
	for _, r := range rows {
		urls := []string{}
		_ = json.Unmarshal([]byte(r.ImageURLsJSON), &urls)
		files := []string{}
		_ = json.Unmarshal([]byte(r.SavedFilesJSON), &files)
		out = append(out, GenerationLogDTO{
			ID:                   r.ID,
			Provider:             r.Provider,
			ProviderGenerationID: r.ProviderGenerationID,
			ProviderAccountID:    r.ProviderAccountID,
			MediaType:            r.MediaType,
			MetadataJSON:         r.MetadataJSON,
			UsedCookieID:         r.UsedCookieID,
			ModelID:              r.ModelID,
			AspectRatio:          r.AspectRatio,
			Prompt:               r.Prompt,
			ImageURLs:            urls,
			SavedFiles:           files,
			SaveEnabled:          r.SaveEnabled == 1,
			Status:               r.Status,
			ErrorMessage:         r.ErrorMessage,
			CreatedAt:            r.CreatedAt,
		})
	}
	return out, nil
}

// AspectRatioOption is one supported aspect ratio entry for the image UI.
type AspectRatioOption struct {
	Label  string `json:"label"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ListImageAspects returns supported aspect ratios for image generation.
func (a *App) ListImageAspects() []AspectRatioOption {
	out := make([]AspectRatioOption, 0, len(service.AspectSize))
	// Stable order for the UI.
	order := []string{"1:1", "16:9", "9:16", "4:3"}
	for _, key := range order {
		size, ok := service.AspectSize[key]
		if !ok {
			continue
		}
		out = append(out, AspectRatioOption{Label: key, Width: size[0], Height: size[1]})
	}
	return out
}

// ----- Internal helpers ----------------------------------------------------

// defaultDataDir returns the renamed application data directory. On first
// launch it moves the old LeoStudio directory so existing accounts survive the
// rebrand. If the old database is locked, it keeps using that path and retries
// the move on a later launch.
func defaultDataDir() string {
	if env := strings.TrimSpace(os.Getenv("ANAN_VIDEO_TOOLBOX_DATA_DIR")); env != "" {
		return env
	}
	if env := strings.TrimSpace(os.Getenv("LEOSTUDIO_DATA_DIR")); env != "" {
		return env
	}
	cfg, err := os.UserConfigDir()
	if err != nil || cfg == "" {
		return "data"
	}
	current := filepath.Join(cfg, "anan-video-toolbox")
	legacy := filepath.Join(cfg, "leostudio")
	currentDB := filepath.Join(current, "app.db")
	legacyDB := filepath.Join(legacy, "app.db")
	preferred := store.PreferredDBPath(currentDB, legacyDB)
	if preferred == legacyDB {
		return legacy
	}
	if _, err := os.Stat(currentDB); err == nil {
		return current
	}
	if _, err := os.Stat(legacyDB); err != nil {
		return current
	}
	if entries, err := os.ReadDir(current); err == nil && len(entries) == 0 {
		_ = os.Remove(current)
	}
	if _, err := os.Stat(current); os.IsNotExist(err) {
		if err := os.Rename(legacy, current); err == nil {
			return current
		}
	}
	return legacy
}
