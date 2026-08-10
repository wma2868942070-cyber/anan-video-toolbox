//go:build windows

package leonardo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type BrowserWorkspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Browser      string `json:"browser"`
	Bound        bool   `json:"bound"`
	AccountID    string `json:"-"`
	ProfileDir   string `json:"profileDir"`
	DebugPort    int    `json:"debugPort"`
	PID          int    `json:"pid"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
}

type storedBrowserWorkspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Browser      string `json:"browser"`
	AccountID    string `json:"accountId,omitempty"`
	ProfileDir   string `json:"profileDir"`
	DebugPort    int    `json:"debugPort"`
	PID          int    `json:"pid"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
}

type BrowserWorkspaceManager struct {
	stateDir string
}

func NewBrowserWorkspaceManager(dataDir string) *BrowserWorkspaceManager {
	return &BrowserWorkspaceManager{stateDir: filepath.Join(dataDir, "leonardo-browser")}
}

func (m *BrowserWorkspaceManager) List() ([]BrowserWorkspace, error) {
	var stored []storedBrowserWorkspace
	raw, err := os.ReadFile(m.workspaceFile())
	if errors.Is(err, os.ErrNotExist) {
		return []BrowserWorkspace{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	items := make([]BrowserWorkspace, 0, len(stored))
	for _, item := range stored {
		items = append(items, BrowserWorkspace{
			ID: item.ID, Name: item.Name, Browser: item.Browser, AccountID: item.AccountID,
			Bound:      strings.TrimSpace(item.AccountID) != "",
			ProfileDir: item.ProfileDir, DebugPort: item.DebugPort, PID: item.PID, LastOpenedAt: item.LastOpenedAt,
		})
	}
	return items, nil
}

// EnsureBoundWorkspace creates a metadata-only workspace for an account that
// was added outside Chrome (for example by cURL). It does not launch Chrome;
// the user can open the card and complete that account's login later.
func (m *BrowserWorkspaceManager) EnsureBoundWorkspace(accountID, name string) (bool, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return false, nil
	}
	items, err := m.List()
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.AccountID) == accountID {
			return false, nil
		}
	}
	workspace := newBrowserWorkspace(items, name)
	workspace.AccountID = accountID
	workspace.Bound = true
	return true, m.save(append(items, workspace))
}

// Get returns one workspace including its internal stable-account binding.
// AccountID remains excluded from the public JSON representation.
func (m *BrowserWorkspaceManager) Get(id string) (BrowserWorkspace, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BrowserWorkspace{}, errors.New("Leonardo 账号工作区不存在")
	}
	items, err := m.List()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return BrowserWorkspace{}, errors.New("Leonardo 账号工作区不存在")
}

func (m *BrowserWorkspaceManager) Launch(name string) (BrowserWorkspace, error) {
	items, err := m.List()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	// "新建并登录" must never reuse an older profile just because the user
	// typed the same display name. Every click receives a cryptographically
	// random workspace id and therefore a different Chrome user-data-dir.
	workspace := newBrowserWorkspace(items, name)
	items = append(items, workspace)
	return m.launchWorkspace(items, len(items)-1)
}

// Reopen launches exactly one existing workspace by its opaque id. Keeping
// this separate from Launch prevents a new account from accidentally reusing
// an old Chrome profile when two workspace notes happen to be identical.
func (m *BrowserWorkspaceManager) Reopen(id string) (BrowserWorkspace, error) {
	items, err := m.List()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	id = strings.TrimSpace(id)
	for i := range items {
		if items[i].ID == id {
			return m.launchWorkspace(items, i)
		}
	}
	return BrowserWorkspace{}, errors.New("Leonardo 账号工作区不存在")
}

// RemoveByAccountID removes every managed workspace bound to one stable
// Leonardo account. Unbound workspaces are retained so a newly opened Chrome
// can still finish its first login/import flow.
func (m *BrowserWorkspaceManager) RemoveByAccountID(accountID string) (int, error) {
	items, err := m.List()
	if err != nil {
		return 0, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, nil
	}
	kept := make([]BrowserWorkspace, 0, len(items))
	removed := make([]BrowserWorkspace, 0, 1)
	for _, item := range items {
		if strings.TrimSpace(item.AccountID) == accountID {
			removed = append(removed, item)
			continue
		}
		kept = append(kept, item)
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := m.save(kept); err != nil {
		return 0, err
	}
	for _, item := range removed {
		m.removeWorkspaceProfile(item)
	}
	return len(removed), nil
}

func (m *BrowserWorkspaceManager) removeWorkspaceProfile(workspace BrowserWorkspace) {
	managedRoot := filepath.Clean(filepath.Join(m.stateDir, "chrome-profiles"))
	expected := filepath.Clean(filepath.Join(managedRoot, workspace.ID))
	actual := filepath.Clean(strings.TrimSpace(workspace.ProfileDir))
	// Never recursively remove an arbitrary path read from the state file.
	// Only the exact directory derived from this manager and workspace id is
	// eligible for best-effort cleanup.
	if workspace.ID == "" || actual == "." || !strings.EqualFold(actual, expected) {
		return
	}
	_ = os.RemoveAll(actual)
}

func newBrowserWorkspace(items []BrowserWorkspace, name string) BrowserWorkspace {
	id := workspaceID(8)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Leonardo账号-" + id[:4]
	} else {
		base := name
		for _, item := range items {
			if strings.EqualFold(strings.TrimSpace(item.Name), name) {
				name = base + "-" + id[:4]
				break
			}
		}
	}
	return BrowserWorkspace{ID: id, Name: name}
}

func (m *BrowserWorkspaceManager) launchWorkspace(items []BrowserWorkspace, index int) (BrowserWorkspace, error) {
	if index < 0 || index >= len(items) {
		return BrowserWorkspace{}, errors.New("Leonardo 账号工作区不存在")
	}
	workspace := &items[index]

	// Keep Chrome profiles separate from the original Edge profiles. Reusing a
	// profile directory across two Chromium products can leave incompatible
	// preferences, locks, or login state behind and was the main reason a newly
	// opened Leonardo workspace appeared unusable after switching browsers.
	profileDir := filepath.Join(m.stateDir, "chrome-profiles", workspace.ID)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return BrowserWorkspace{}, err
	}
	chrome, err := workspaceChromePath()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	if strings.EqualFold(workspace.Browser, "chrome") && workspace.DebugPort > 0 && workspaceDebugAvailable(workspace.DebugPort) {
		// The dedicated Chrome is already running. Reuse its original debugging
		// port and only ask Chrome to open/activate Leonardo; overwriting the port
		// here would make the subsequent one-click Cookie capture connect to a
		// port that no process is listening on.
		_ = exec.Command(chrome,
			"--user-data-dir="+profileDir,
			"--no-first-run",
			"--no-default-browser-check",
			"https://app.leonardo.ai/",
		).Start()
		workspace.Browser = "chrome"
		workspace.ProfileDir = profileDir
		workspace.LastOpenedAt = time.Now().Unix()
		if err := m.save(items); err != nil {
			return BrowserWorkspace{}, err
		}
		return *workspace, nil
	}
	port, err := workspaceFreePort()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	cmd := exec.Command(chrome,
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+profileDir,
		"--profile-directory=Default",
		"--disable-background-mode",
		"--no-first-run",
		"--no-default-browser-check",
		"https://app.leonardo.ai/",
	)
	if err := cmd.Start(); err != nil {
		return BrowserWorkspace{}, fmt.Errorf("启动 Google Chrome 失败: %w", err)
	}
	if !waitForWorkspaceDebugPort(port, 12*time.Second) {
		_ = cmd.Process.Kill()
		return BrowserWorkspace{}, errors.New("Google Chrome 已启动，但隔离账号调试端口未就绪；请关闭该工作区的 Chrome 后重试")
	}
	workspace.Browser = "chrome"
	workspace.ProfileDir = profileDir
	workspace.DebugPort = port
	workspace.PID = cmd.Process.Pid
	workspace.LastOpenedAt = time.Now().Unix()
	if err := m.save(items); err != nil {
		return BrowserWorkspace{}, err
	}
	return *workspace, nil
}

func (m *BrowserWorkspaceManager) ReadCookie(ctx context.Context, id string) (string, error) {
	items, err := m.List()
	if err != nil {
		return "", err
	}
	var workspace *BrowserWorkspace
	for i := range items {
		if items[i].ID == id {
			workspace = &items[i]
			break
		}
	}
	if workspace == nil {
		return "", errors.New("Leonardo 账号工作区不存在")
	}
	if !strings.EqualFold(workspace.Browser, "chrome") {
		return "", errors.New("该工作区仍是旧 Edge 工作区，请先点击“重新打开”迁移到 Google Chrome，再读取 Cookie")
	}
	var cookie string
	if workspace.DebugPort > 0 && workspaceDebugAvailable(workspace.DebugPort) {
		cookie, err = readLeonardoBrowserCookie(ctx, workspace.DebugPort)
	} else {
		// A dedicated account browser does not need to remain open forever. Start
		// its persistent profile briefly in headless mode, read the newest HttpOnly
		// Better Auth cookies through CDP, then close it again. This lets the
		// background session recovery repair an expired stored Cookie without
		// opening a visible Chrome window or asking the user to copy cURL again.
		cookie, err = readLeonardoClosedWorkspaceCookie(ctx, *workspace)
	}
	if err != nil {
		return "", err
	}
	if cookie == "" {
		return "", errors.New("未读取到 Leonardo Cookie，请确认已在该隔离 Google Chrome 中完成登录")
	}
	return cookie, nil
}

// BindAccount records the stable Leonardo account represented by a managed
// Chrome profile. AccountID is intentionally omitted from the public JSON DTO.
// One account may have only one recovery workspace, otherwise two duplicate
// profiles could both report success while other accounts remain unrecoverable.
func (m *BrowserWorkspaceManager) BindAccount(id, accountID string) error {
	items, err := m.List()
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	accountID = strings.TrimSpace(accountID)
	if id == "" || accountID == "" {
		return errors.New("Leonardo 工作区或账号身份无效")
	}
	found := false
	for i := range items {
		if items[i].ID == id {
			found = true
			if bound := strings.TrimSpace(items[i].AccountID); bound != "" && bound != accountID {
				return errors.New("该 Chrome 工作区已绑定另一个 Leonardo 账号；请新建独立工作区登录当前账号")
			}
			items[i].AccountID = accountID
			continue
		}
		if strings.TrimSpace(items[i].AccountID) == accountID {
			return fmt.Errorf("%w：当前登录账号已绑定工作区“%s”；Cookie 已识别为同一账号，不会重复添加", ErrAccountWorkspaceAlreadyBound, items[i].Name)
		}
	}
	if !found {
		return errors.New("Leonardo 账号工作区不存在")
	}
	return m.save(items)
}

func readLeonardoClosedWorkspaceCookie(ctx context.Context, workspace BrowserWorkspace) (string, error) {
	profileDir := strings.TrimSpace(workspace.ProfileDir)
	if profileDir == "" {
		return "", errors.New("Leonardo Google Chrome 工作区缺少用户目录")
	}
	if _, err := os.Stat(profileDir); err != nil {
		return "", errors.New("Leonardo Google Chrome 工作区目录不存在")
	}
	chrome, err := workspaceChromePath()
	if err != nil {
		return "", err
	}
	port, err := workspaceFreePort()
	if err != nil {
		return "", err
	}
	childCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, chrome,
		"--headless=new",
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+profileDir,
		"--profile-directory=Default",
		"--disable-background-mode",
		"--no-first-run",
		"--no-default-browser-check",
		"https://app.leonardo.ai/api/auth/get-session",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("后台启动 Leonardo Google Chrome 工作区失败: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitForWorkspaceDebugPort(port, 12*time.Second) {
		return "", errors.New("Leonardo Google Chrome 工作区后台读取超时；可能仍有同一工作区窗口占用用户目录")
	}
	return readLeonardoBrowserCookie(childCtx, port)
}

func (m *BrowserWorkspaceManager) workspaceFile() string {
	return filepath.Join(m.stateDir, "workspaces.json")
}

func (m *BrowserWorkspaceManager) save(items []BrowserWorkspace) error {
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return err
	}
	stored := make([]storedBrowserWorkspace, 0, len(items))
	for _, item := range items {
		stored = append(stored, storedBrowserWorkspace{
			ID: item.ID, Name: item.Name, Browser: item.Browser, AccountID: item.AccountID,
			ProfileDir: item.ProfileDir, DebugPort: item.DebugPort, PID: item.PID, LastOpenedAt: item.LastOpenedAt,
		})
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.workspaceFile(), raw, 0o600)
}

func readLeonardoBrowserCookie(ctx context.Context, port int) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json", port), nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接 Leonardo 隔离 Google Chrome，请保持浏览器打开: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Leonardo 隔离 Google Chrome 调试端口返回 HTTP %d", resp.StatusCode)
	}
	var targets []struct {
		URL          string `json:"url"`
		Type         string `json:"type"`
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", errors.New("无法读取 Leonardo 隔离 Google Chrome 页面")
	}
	webSocketURL := ""
	for _, target := range targets {
		if target.Type == "page" && strings.Contains(strings.ToLower(target.URL), "leonardo.ai") {
			webSocketURL = target.WebSocketURL
			break
		}
	}
	if webSocketURL == "" {
		for _, target := range targets {
			if target.Type == "page" && target.WebSocketURL != "" {
				webSocketURL = target.WebSocketURL
				break
			}
		}
	}
	if webSocketURL == "" {
		return "", errors.New("隔离 Google Chrome 中没有可读取的 Leonardo 页面")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, webSocketURL, nil)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	if err := conn.WriteJSON(map[string]any{
		"id": 1, "method": "Network.getCookies",
		"params": map[string]any{"urls": []string{"https://app.leonardo.ai/api/auth/get-session"}},
	}); err != nil {
		return "", err
	}
	for {
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			return "", err
		}
		if int(workspaceNumber(message["id"])) != 1 {
			continue
		}
		result, _ := message["result"].(map[string]any)
		rawCookies, _ := result["cookies"].([]any)
		return formatLeonardoCookieHeader(rawCookies), nil
	}
}

type leonardoBrowserCookie struct {
	Name   string
	Value  string
	Domain string
	Path   string
}

func formatLeonardoCookieHeader(rawCookies []any) string {
	// Match leo2api's exporter: one effective value per Cookie name, preferring
	// the exact app.leonardo.ai host and the most specific path. Sending two
	// session_token values is ambiguous and can make the backend resolve the
	// wrong account even though the browser itself is logged in correctly.
	best := make(map[string]leonardoBrowserCookie)
	for _, raw := range rawCookies {
		cookie, _ := raw.(map[string]any)
		domain := strings.ToLower(strings.TrimSpace(fmt.Sprint(cookie["domain"])))
		host := strings.TrimPrefix(domain, ".")
		name := strings.TrimSpace(fmt.Sprint(cookie["name"]))
		value := fmt.Sprint(cookie["value"])
		path := strings.TrimSpace(fmt.Sprint(cookie["path"]))
		if name == "" || value == "" || (host != "leonardo.ai" && !strings.HasSuffix(host, ".leonardo.ai")) || !isLeonardoSessionCookie(name) {
			continue
		}
		candidate := leonardoBrowserCookie{Name: name, Value: value, Domain: domain, Path: path}
		if current, ok := best[name]; !ok || leonardoBrowserCookieSpecificity(candidate) > leonardoBrowserCookieSpecificity(current) {
			best[name] = candidate
		}
	}
	cookies := make([]leonardoBrowserCookie, 0, len(best))
	for _, cookie := range best {
		cookies = append(cookies, cookie)
	}
	sort.SliceStable(cookies, func(i, j int) bool {
		leftPriority := leonardoCookiePriority(cookies[i].Name)
		rightPriority := leonardoCookiePriority(cookies[j].Name)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if cookies[i].Name != cookies[j].Name {
			return cookies[i].Name < cookies[j].Name
		}
		if len(cookies[i].Path) != len(cookies[j].Path) {
			return len(cookies[i].Path) > len(cookies[j].Path)
		}
		return cookies[i].Domain < cookies[j].Domain
	})
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func leonardoBrowserCookieSpecificity(cookie leonardoBrowserCookie) int {
	score := len(cookie.Path)
	if strings.EqualFold(strings.TrimPrefix(cookie.Domain, "."), "app.leonardo.ai") && !strings.HasPrefix(cookie.Domain, ".") {
		score += 100000
	}
	return score
}

func isLeonardoSessionCookie(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "__secure-better-auth.session_token" ||
		strings.HasPrefix(lower, "__secure-better-auth.session_data.") ||
		strings.Contains(lower, "next-auth") || strings.Contains(lower, "authjs") ||
		lower == "cf_access_token" || lower == "cf_clearance" || lower == "__cf_bm"
}

func leonardoCookiePriority(name string) int {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "better-auth") || strings.Contains(lower, "next-auth") || strings.Contains(lower, "authjs") {
		return 0
	}
	if lower == "cf_clearance" || lower == "__cf_bm" {
		return 1
	}
	return 2
}

func workspaceDebugAvailable(port int) bool {
	client := &http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func waitForWorkspaceDebugPort(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if workspaceDebugAvailable(port) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func workspaceChromePath() (string, error) {
	for _, candidate := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if strings.TrimSpace(filepath.Dir(candidate)) == "." {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("未找到 Google Chrome，请先安装 Chrome 后再使用 Leonardo 隔离账号库")
}

func workspaceFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func workspaceID(size int) string {
	raw := make([]byte, size)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)[:size]
}

func workspaceNumber(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		value, _ := n.Float64()
		return value
	default:
		return 0
	}
}
