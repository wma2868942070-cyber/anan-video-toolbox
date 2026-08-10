//go:build windows

package videoclaw

import (
	"context"
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
)

const (
	backendPort  = 6101
	frontendPort = 6102
)

type Manager struct{}

type ServiceStatus struct {
	Running         bool   `json:"running"`
	BackendRunning  bool   `json:"backendRunning"`
	FrontendRunning bool   `json:"frontendRunning"`
	BackendURL      string `json:"backendURL"`
	FrontendURL     string `json:"frontendURL"`
	StateDir        string `json:"stateDir"`
	SourceDir       string `json:"sourceDir"`
	PythonPath      string `json:"pythonPath"`
	Error           string `json:"error"`
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) Status(ctx context.Context) ServiceStatus {
	status := ServiceStatus{
		BackendURL:  fmt.Sprintf("http://127.0.0.1:%d", backendPort),
		FrontendURL: fmt.Sprintf("http://127.0.0.1:%d", frontendPort),
		StateDir:    stateDir(),
		SourceDir:   sourceDir(),
		PythonPath:  pythonPath(),
	}
	status.BackendRunning = backendHealthy(ctx, status.BackendURL)
	status.FrontendRunning = frontendHealthy(ctx, status.FrontendURL)
	status.Running = status.BackendRunning && status.FrontendRunning
	if raw, err := os.ReadFile(filepath.Join(status.StateDir, "startup-error.txt")); err == nil {
		status.Error = strings.TrimSpace(string(raw))
	}
	if status.Running {
		status.Error = ""
	}
	return status
}

func (m *Manager) Start(ctx context.Context) (ServiceStatus, error) {
	status := m.Status(ctx)
	if status.Running {
		return status, nil
	}
	script, err := startScriptPath()
	if err != nil {
		return status, err
	}
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.Dir = filepath.Dir(filepath.Dir(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 1000 {
			message = message[len(message)-1000:]
		}
		if message == "" {
			message = err.Error()
		}
		writeStartupError(message)
		return m.Status(context.Background()), fmt.Errorf("VideoClaw 启动失败: %s", message)
	}
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		status = m.Status(context.Background())
		if status.Running {
			return status, nil
		}
	}
	message := "VideoClaw 启动完成，但健康检查仍未通过"
	writeStartupError(message)
	return status, errors.New(message)
}

func (m *Manager) Restart(ctx context.Context) (ServiceStatus, error) {
	for _, name := range []string{"backend.pid", "frontend.pid"} {
		raw, _ := os.ReadFile(filepath.Join(stateDir(), name))
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		if pid <= 0 {
			continue
		}
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	}
	for i := 0; i < 40; i++ {
		if !tcpPortListening(backendPort) && !tcpPortListening(frontendPort) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if tcpPortListening(backendPort) || tcpPortListening(frontendPort) {
		message := "VideoClaw 进程未能完全停止，请检查端口 6101/6102"
		writeStartupError(message)
		return m.Status(ctx), errors.New(message)
	}
	return m.Start(ctx)
}

func writeStartupError(message string) {
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(stateDir(), "startup-error.txt"), []byte(strings.TrimSpace(message)), 0o600)
}

func backendHealthy(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/health", nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Status string `json:"status"`
	}
	return json.NewDecoder(resp.Body).Decode(&payload) == nil && payload.Status == "ok"
}

func frontendHealthy(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func stateDir() string {
	if value := strings.TrimSpace(os.Getenv("VIDEOCLAW_STATE_DIR")); value != "" {
		return value
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "anan-video-toolbox", "videoclaw")
}

func pythonPath() string {
	return filepath.Join(stateDir(), ".venv", "Scripts", "python.exe")
}

func sourceDir() string {
	if value := strings.TrimSpace(os.Getenv("VIDEOCLAW_SOURCE_DIR")); value != "" {
		return value
	}
	cwd, _ := os.Getwd()
	for i := 0; i < 7 && cwd != ""; i++ {
		candidate := filepath.Join(cwd, "third_party", "videoclaw")
		if _, err := os.Stat(filepath.Join(candidate, "backend", "api_server.py")); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	return filepath.Join(cwd, "third_party", "videoclaw")
}

func startScriptPath() (string, error) {
	source := sourceDir()
	root := filepath.Dir(filepath.Dir(source))
	script := filepath.Join(root, "scripts", "start-videoclaw.ps1")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("找不到 VideoClaw 启动脚本: %s", script)
	}
	return script, nil
}

func tcpPortListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
