//go:build windows

package adobe

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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Manager struct {
	Client *Client
}

type ServiceStatus struct {
	Running      bool   `json:"running"`
	BaseURL      string `json:"baseURL"`
	StateDir     string `json:"stateDir"`
	SourceDir    string `json:"sourceDir"`
	PythonPath   string `json:"pythonPath"`
	PoolSize     int    `json:"poolSize"`
	Error        string `json:"error"`
	AdminURL     string `json:"adminURL"`
	CookiePlugin string `json:"cookiePlugin"`
}

type BrowserWorkspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Browser      string `json:"browser"`
	ProfileDir   string `json:"profileDir"`
	DebugPort    int    `json:"debugPort"`
	PID          int    `json:"pid"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
}

func NewManager() *Manager { return &Manager{Client: NewClient()} }

func (m *Manager) Status(ctx context.Context) ServiceStatus {
	status := ServiceStatus{
		BaseURL:      m.Client.BaseURL,
		StateDir:     m.Client.StateDir,
		SourceDir:    sourceDir(),
		PythonPath:   pythonPath(),
		AdminURL:     m.Client.BaseURL + "/",
		CookiePlugin: filepath.Join(sourceDir(), "browser-cookie-exporter"),
	}
	payload, err := m.Client.Health(ctx)
	if err != nil {
		status.Error = err.Error()
		if raw, readErr := os.ReadFile(filepath.Join(m.Client.StateDir, "startup-error.txt")); readErr == nil {
			if startupError := strings.TrimSpace(string(raw)); startupError != "" {
				status.Error = startupError
			}
		}
		return status
	}
	status.Running = true
	status.PoolSize = int(number(payload["pool_size"]))
	return status
}

func (m *Manager) Start(ctx context.Context) (ServiceStatus, error) {
	status := m.Status(ctx)
	if status.Running {
		return status, nil
	}
	source := sourceDir()
	python := pythonPath()
	if _, err := os.Stat(filepath.Join(source, "app.py")); err != nil {
		return status, fmt.Errorf("找不到 Adobe2API 源码: %s", source)
	}
	if _, err := os.Stat(python); err != nil {
		return status, fmt.Errorf("Adobe2API Python 环境尚未初始化: %s", python)
	}
	if tcpPortListening(6001) {
		return status, errors.New("端口 6001 已被占用，但占用进程不是可用的 Adobe2API Sidecar")
	}
	if err := os.MkdirAll(m.Client.StateDir, 0o755); err != nil {
		return status, err
	}
	logDir := filepath.Join(m.Client.StateDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	out, err := os.OpenFile(filepath.Join(logDir, "sidecar.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return status, err
	}
	cmd := exec.CommandContext(ctx, python, "-m", "uvicorn", "app:app", "--host", "127.0.0.1", "--port", "6001")
	cmd.Dir = source
	cmd.Env = append(os.Environ(),
		"ADOBE2API_STATE_DIR="+m.Client.StateDir,
		"PYTHONDONTWRITEBYTECODE=1",
	)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		return status, err
	}
	_ = out.Close()
	_ = os.WriteFile(filepath.Join(m.Client.StateDir, "sidecar.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		status = m.Status(context.Background())
		if status.Running {
			_ = os.Remove(filepath.Join(m.Client.StateDir, "startup-error.txt"))
			return status, nil
		}
	}
	return status, errors.New("Adobe2API 启动超时，请查看 sidecar.log")
}

func (m *Manager) ResetAdminPassword(ctx context.Context) (string, error) {
	password := randomID(24)
	if _, err := m.Client.AdminJSON(ctx, http.MethodPut, "/api/v1/config", map[string]any{
		"admin_password": password,
	}); err != nil {
		return "", err
	}
	return password, nil
}

func (m *Manager) Restart(ctx context.Context) (ServiceStatus, error) {
	pidRaw, _ := os.ReadFile(filepath.Join(m.Client.StateDir, "sidecar.pid"))
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(pidRaw))); pid > 0 {
		// Windows virtual environments use a small python.exe redirector which
		// starts the real interpreter as a child process. Killing only the PID in
		// sidecar.pid can leave that child listening on 6001, making "restart"
		// appear successful without actually reloading the Sidecar.
		kill := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = kill.Run()
		for i := 0; i < 20 && tcpPortListening(6001); i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if tcpPortListening(6001) {
		return m.Status(ctx), errors.New("Adobe2API 进程未能完全停止，端口 6001 仍被占用")
	}
	return m.Start(ctx)
}

func (m *Manager) ListWorkspaces() ([]BrowserWorkspace, error) {
	var out []BrowserWorkspace
	raw, err := os.ReadFile(m.workspaceFile())
	if errors.Is(err, os.ErrNotExist) {
		return []BrowserWorkspace{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	// Older builds created Adobe workspaces with Edge and did not persist a
	// browser field. Keep those rows readable, but all newly opened workspaces
	// are migrated to a dedicated Chrome profile by LaunchWorkspace.
	for i := range out {
		if strings.TrimSpace(out[i].Browser) == "" {
			out[i].Browser = "edge"
		}
	}
	return out, nil
}

func (m *Manager) LaunchWorkspace(name string) (BrowserWorkspace, error) {
	items, err := m.ListWorkspaces()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	name = strings.TrimSpace(name)
	var workspace *BrowserWorkspace
	for i := range items {
		if strings.EqualFold(items[i].Name, name) && name != "" {
			workspace = &items[i]
			break
		}
	}
	if workspace == nil {
		id := randomID(8)
		if name == "" {
			name = "Adobe账号-" + id[:4]
		}
		items = append(items, BrowserWorkspace{ID: id, Name: name})
		workspace = &items[len(items)-1]
	}
	port, err := freePort()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	// Do not reuse an Edge Chromium profile. Chrome and Edge can leave
	// incompatible locks/preferences behind and the user explicitly wants the
	// Adobe account pool to use Google Chrome as its managed browser.
	profileDir := filepath.Join(m.Client.StateDir, "chrome-browser-profiles", workspace.ID)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return BrowserWorkspace{}, err
	}
	chrome, err := chromePath()
	if err != nil {
		return BrowserWorkspace{}, err
	}
	wasChrome := strings.EqualFold(workspace.Browser, "chrome") && strings.EqualFold(workspace.ProfileDir, profileDir)
	workspace.ProfileDir = profileDir
	workspace.Browser = "chrome"
	if workspace.DebugPort > 0 && workspaceDebugAvailable(workspace.DebugPort) && wasChrome {
		_ = exec.Command(chrome,
			"--user-data-dir="+profileDir,
			"--no-first-run",
			"--no-default-browser-check",
			"https://firefly.adobe.com/",
		).Start()
		workspace.LastOpenedAt = time.Now().Unix()
		if err := m.saveWorkspaces(items); err != nil {
			return BrowserWorkspace{}, err
		}
		return *workspace, nil
	}
	args := []string{
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + profileDir,
		"--no-first-run",
	}
	if plugin := filepath.Join(sourceDir(), "browser-cookie-exporter"); dirExists(plugin) {
		args = append(args, "--load-extension="+plugin)
	}
	args = append(args, "https://firefly.adobe.com/")
	cmd := exec.Command(chrome, args...)
	if err := cmd.Start(); err != nil {
		return BrowserWorkspace{}, err
	}
	workspace.ProfileDir = profileDir
	workspace.Browser = "chrome"
	workspace.DebugPort = port
	workspace.PID = cmd.Process.Pid
	workspace.LastOpenedAt = time.Now().Unix()
	if err := m.saveWorkspaces(items); err != nil {
		return BrowserWorkspace{}, err
	}
	return *workspace, nil
}

func (m *Manager) ImportWorkspaceCookies(ctx context.Context, id string) (map[string]any, error) {
	items, err := m.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var workspace *BrowserWorkspace
	for i := range items {
		if items[i].ID == id {
			workspace = &items[i]
			break
		}
	}
	if workspace == nil || workspace.DebugPort == 0 {
		return nil, errors.New("账号工作区不存在或浏览器尚未打开")
	}
	cookies, err := readAdobeCookies(ctx, workspace.DebugPort)
	if err != nil {
		return nil, err
	}
	if len(cookies) == 0 {
		return nil, errors.New("未读取到 Adobe Cookie，请确认已经完成登录")
	}
	return m.Client.AdminJSON(ctx, http.MethodPost, "/api/v1/refresh-profiles/import-cookie", map[string]any{
		"name":   workspace.Name,
		"cookie": cookies,
	})
}

func (m *Manager) workspaceFile() string {
	return filepath.Join(m.Client.StateDir, "browser-workspaces.json")
}

func (m *Manager) saveWorkspaces(items []BrowserWorkspace) error {
	if err := os.MkdirAll(m.Client.StateDir, 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(items, "", "  ")
	return os.WriteFile(m.workspaceFile(), raw, 0o600)
}

func readAdobeCookies(ctx context.Context, port int) ([]map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json", port), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接受管 Google Chrome，请保持浏览器打开: %w", err)
	}
	defer resp.Body.Close()
	var targets []struct {
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil || len(targets) == 0 {
		return nil, errors.New("受管 Google Chrome 没有可读取的页面")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, targets[0].WebSocketURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	request := map[string]any{"id": 1, "method": "Network.getAllCookies"}
	if err := conn.WriteJSON(request); err != nil {
		return nil, err
	}
	for {
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			return nil, err
		}
		if int(number(message["id"])) != 1 {
			continue
		}
		result, _ := message["result"].(map[string]any)
		rawCookies, _ := result["cookies"].([]any)
		cookies := make([]map[string]any, 0, len(rawCookies))
		for _, raw := range rawCookies {
			cookie, _ := raw.(map[string]any)
			domain := strings.ToLower(fmt.Sprint(cookie["domain"]))
			if !strings.Contains(domain, "adobe.com") {
				continue
			}
			cookies = append(cookies, map[string]any{
				"name":  fmt.Sprint(cookie["name"]),
				"value": fmt.Sprint(cookie["value"]),
			})
		}
		return cookies, nil
	}
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

func sourceDir() string {
	if value := strings.TrimSpace(os.Getenv("ADOBE2API_SOURCE_DIR")); value != "" {
		return value
	}
	cwd, _ := os.Getwd()
	for i := 0; i < 6 && cwd != ""; i++ {
		candidate := filepath.Join(cwd, "third_party", "adobe2api")
		if _, err := os.Stat(filepath.Join(candidate, "app.py")); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	return filepath.Join(cwd, "third_party", "adobe2api")
}

func pythonPath() string {
	if value := strings.TrimSpace(os.Getenv("ADOBE2API_PYTHON")); value != "" {
		return value
	}
	return filepath.Join(StateDir(), ".venv", "Scripts", "python.exe")
}

func chromePath() (string, error) {
	for _, candidate := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("未找到 Google Chrome，请先安装 Chrome 后再使用 Adobe 隔离账号库")
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func tcpPortListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func randomID(size int) string {
	raw := make([]byte, size)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)[:size]
}

func number(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		v, _ := n.Float64()
		return v
	default:
		v, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return v
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
