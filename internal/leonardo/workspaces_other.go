//go:build !windows

package leonardo

import (
	"context"
	"errors"
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

type BrowserWorkspaceManager struct{}

func NewBrowserWorkspaceManager(string) *BrowserWorkspaceManager   { return &BrowserWorkspaceManager{} }
func (*BrowserWorkspaceManager) List() ([]BrowserWorkspace, error) { return []BrowserWorkspace{}, nil }
func (*BrowserWorkspaceManager) Get(string) (BrowserWorkspace, error) {
	return BrowserWorkspace{}, errors.New("Leonardo 隔离账号库目前仅支持 Windows")
}
func (*BrowserWorkspaceManager) Launch(string) (BrowserWorkspace, error) {
	return BrowserWorkspace{}, errors.New("Leonardo 隔离账号库目前仅支持 Windows")
}
func (*BrowserWorkspaceManager) Reopen(string) (BrowserWorkspace, error) {
	return BrowserWorkspace{}, errors.New("Leonardo 隔离账号库目前仅支持 Windows")
}
func (*BrowserWorkspaceManager) RemoveByAccountID(string) (int, error)             { return 0, nil }
func (*BrowserWorkspaceManager) EnsureBoundWorkspace(string, string) (bool, error) { return false, nil }
func (*BrowserWorkspaceManager) ReadCookie(context.Context, string) (string, error) {
	return "", errors.New("Leonardo 隔离账号库目前仅支持 Windows")
}
func (*BrowserWorkspaceManager) BindAccount(string, string) error {
	return errors.New("Leonardo 隔离账号库目前仅支持 Windows")
}
