//go:build !windows

package localminidrama

import (
	"context"
	"errors"
)

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
func (m *Manager) Status(context.Context) ServiceStatus {
	return ServiceStatus{Error: "LocalMiniDrama Sidecar 当前仅支持 Windows"}
}
func (m *Manager) Start(context.Context) (ServiceStatus, error) {
	return m.Status(context.Background()), errors.New("LocalMiniDrama Sidecar 当前仅支持 Windows")
}
func (m *Manager) Restart(context.Context) (ServiceStatus, error) {
	return m.Start(context.Background())
}
func (m *Manager) SyncModels(context.Context) (map[string]any, error) {
	return nil, errors.New("LocalMiniDrama Sidecar 当前仅支持 Windows")
}
