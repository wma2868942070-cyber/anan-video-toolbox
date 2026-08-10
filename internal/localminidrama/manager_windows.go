//go:build windows

package localminidrama

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

const servicePort = 6201

type Manager struct{}

type ModelConfigStatus struct {
	Name         string `json:"name"`
	ServiceType  string `json:"service_type"`
	ModelCount   int    `json:"model_count"`
	DefaultModel string `json:"default_model"`
	IsActive     bool   `json:"is_active"`
	UpdatedAt    string `json:"updated_at"`
}

type ServiceStatus struct {
	Running      bool                `json:"running"`
	URL          string              `json:"url"`
	StateDir     string              `json:"stateDir"`
	SourceDir    string              `json:"sourceDir"`
	NodePath     string              `json:"nodePath"`
	Error        string              `json:"error"`
	ModelConfigs []ModelConfigStatus `json:"modelConfigs"`
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) Status(ctx context.Context) ServiceStatus {
	status := ServiceStatus{
		URL:       fmt.Sprintf("http://127.0.0.1:%d", servicePort),
		StateDir:  stateDir(),
		SourceDir: sourceDir(),
		NodePath:  nodePath(),
	}
	status.Running = healthy(ctx, status.URL)
	if status.Running {
		status.ModelConfigs, _ = modelConfigStatus(ctx, status.URL)
	} else if raw, err := os.ReadFile(filepath.Join(status.StateDir, "startup-error.txt")); err == nil {
		status.Error = strings.TrimSpace(string(raw))
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
		message := tailMessage(output, err.Error())
		writeStartupError(message)
		return m.Status(context.Background()), fmt.Errorf("LocalMiniDrama 启动失败: %s", message)
	}
	for i := 0; i < 60; i++ {
		time.Sleep(250 * time.Millisecond)
		status = m.Status(context.Background())
		if status.Running {
			return status, nil
		}
	}
	message := "LocalMiniDrama 启动完成，但 6201 健康检查仍未通过"
	writeStartupError(message)
	return status, errors.New(message)
}

func (m *Manager) Restart(ctx context.Context) (ServiceStatus, error) {
	raw, _ := os.ReadFile(filepath.Join(stateDir(), "server.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid > 0 {
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	}
	for i := 0; i < 40 && tcpPortListening(servicePort); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if tcpPortListening(servicePort) {
		message := "LocalMiniDrama 进程未能停止，请检查端口 6201"
		writeStartupError(message)
		return m.Status(ctx), errors.New(message)
	}
	return m.Start(ctx)
}

func (m *Manager) SyncModels(ctx context.Context) (map[string]any, error) {
	status := m.Status(ctx)
	if !status.Running {
		return nil, errors.New("LocalMiniDrama 未运行")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, status.URL+"/api/v1/local-gateway/sync", nil)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("同步 LocalMiniDrama 模型: %w", err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("解析 LocalMiniDrama 同步结果: %w", err)
	}
	if resp.StatusCode >= 300 || !envelope.Success {
		message := strings.TrimSpace(envelope.Error.Message)
		if message == "" {
			message = resp.Status
		}
		return nil, errors.New(message)
	}
	return envelope.Data, nil
}

func modelConfigStatus(ctx context.Context, baseURL string) ([]ModelConfigStatus, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/local-gateway/status", nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var envelope struct {
		Data []ModelConfigStatus `json:"data"`
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

func healthy(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
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

func tailMessage(output []byte, fallback string) string {
	message := strings.TrimSpace(string(output))
	if len(message) > 1500 {
		message = message[len(message)-1500:]
	}
	if message == "" {
		message = fallback
	}
	return message
}

func writeStartupError(message string) {
	if err := os.MkdirAll(stateDir(), 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(stateDir(), "startup-error.txt"), []byte(strings.TrimSpace(message)), 0o600)
	}
}

func stateDir() string {
	if value := strings.TrimSpace(os.Getenv("LOCALMINIDRAMA_STATE_DIR")); value != "" {
		return value
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "anan-video-toolbox", "localminidrama")
}

func nodePath() string {
	runtimeRoot := filepath.Join(os.Getenv("LOCALAPPDATA"), "anan-video-toolbox", "runtimes")
	if raw, err := os.ReadFile(filepath.Join(runtimeRoot, "node22-current.txt")); err == nil {
		candidate := filepath.Join(runtimeRoot, strings.TrimSpace(string(raw)), "node.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	path, _ := exec.LookPath("node.exe")
	return path
}

func sourceDir() string {
	if value := strings.TrimSpace(os.Getenv("LOCALMINIDRAMA_SOURCE_DIR")); value != "" {
		return value
	}
	cwd, _ := os.Getwd()
	for i := 0; i < 7 && cwd != ""; i++ {
		candidate := filepath.Join(cwd, "third_party", "localminidrama")
		if _, err := os.Stat(filepath.Join(candidate, "backend-node", "src", "server.js")); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	return filepath.Join(cwd, "third_party", "localminidrama")
}

func startScriptPath() (string, error) {
	root := filepath.Dir(filepath.Dir(sourceDir()))
	script := filepath.Join(root, "scripts", "start-localminidrama.ps1")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("找不到 LocalMiniDrama 启动脚本: %s", script)
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
