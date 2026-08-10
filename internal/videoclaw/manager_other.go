//go:build !windows

package videoclaw

import (
	"context"
	"errors"
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

func (m *Manager) Status(context.Context) ServiceStatus {
	return ServiceStatus{Error: "VideoClaw Sidecar 当前仅支持 Windows"}
}

func (m *Manager) Start(context.Context) (ServiceStatus, error) {
	return m.Status(context.Background()), errors.New("VideoClaw Sidecar 当前仅支持 Windows")
}

func (m *Manager) Restart(context.Context) (ServiceStatus, error) {
	return m.Start(context.Background())
}
