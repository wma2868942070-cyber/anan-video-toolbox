package adobe

import (
	"path/filepath"
	"testing"
)

func TestStateDirPrefersExplicitOverride(t *testing.T) {
	expected := filepath.Join(t.TempDir(), "explicit-state")
	t.Setenv("ADOBE2API_STATE_DIR", expected)
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "local"))
	if got := StateDir(); got != expected {
		t.Fatalf("StateDir() = %q, want %q", got, expected)
	}
}

func TestStateDirUsesLocalAppDataForDirectDesktopLaunch(t *testing.T) {
	local := filepath.Join(t.TempDir(), "LocalAppData")
	t.Setenv("ADOBE2API_STATE_DIR", "")
	t.Setenv("LOCALAPPDATA", local)
	expected := filepath.Join(local, "anan-video-toolbox", "adobe2api")
	if got := StateDir(); got != expected {
		t.Fatalf("StateDir() = %q, want %q", got, expected)
	}
}
