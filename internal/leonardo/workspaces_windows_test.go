//go:build windows

package leonardo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBrowserWorkspaceManagerPersistsWorkspaces(t *testing.T) {
	manager := NewBrowserWorkspaceManager(t.TempDir())
	want := []BrowserWorkspace{{
		ID: "account01", Name: "Leonardo-01", Bound: true, ProfileDir: `C:\profiles\account01`,
		AccountID: "stable-account-01", DebugPort: 9222, PID: 1234, LastOpenedAt: 123456,
	}}
	if err := manager.save(want); err != nil {
		t.Fatal(err)
	}
	got, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
	if filepath.Base(manager.workspaceFile()) != "workspaces.json" {
		t.Fatalf("unexpected workspace file: %s", manager.workspaceFile())
	}
}

func TestNewBrowserWorkspaceNeverReusesSameNameOrProfile(t *testing.T) {
	first := newBrowserWorkspace(nil, "相同备注")
	second := newBrowserWorkspace([]BrowserWorkspace{first}, "相同备注")
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("workspace ids must be unique: %q %q", first.ID, second.ID)
	}
	if first.Name == second.Name {
		t.Fatalf("duplicate display names must be disambiguated: %q", first.Name)
	}
	root := t.TempDir()
	firstProfile := filepath.Join(root, "chrome-profiles", first.ID)
	secondProfile := filepath.Join(root, "chrome-profiles", second.ID)
	if firstProfile == secondProfile {
		t.Fatalf("workspace profile directories must be unique: %q", firstProfile)
	}
}

func TestRemoveByAccountIDRemovesOnlyBoundWorkspaceAndManagedProfile(t *testing.T) {
	manager := NewBrowserWorkspaceManager(t.TempDir())
	removedProfile := filepath.Join(manager.stateDir, "chrome-profiles", "remove-me")
	keptProfile := filepath.Join(manager.stateDir, "chrome-profiles", "keep-me")
	if err := os.MkdirAll(removedProfile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(keptProfile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(removedProfile, "marker"), []byte("remove"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := []BrowserWorkspace{
		{ID: "remove-me", Name: "remove", AccountID: "stable-remove", ProfileDir: removedProfile},
		{ID: "keep-me", Name: "keep", AccountID: "stable-keep", ProfileDir: keptProfile},
		{ID: "unbound", Name: "new login"},
	}
	if err := manager.save(items); err != nil {
		t.Fatal(err)
	}
	removed, err := manager.RemoveByAccountID("stable-remove")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	got, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "keep-me" || got[1].ID != "unbound" {
		t.Fatalf("unexpected remaining workspaces: %#v", got)
	}
	if _, err := os.Stat(removedProfile); !os.IsNotExist(err) {
		t.Fatalf("removed profile still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(keptProfile); err != nil {
		t.Fatalf("unrelated profile was removed: %v", err)
	}
}

func TestEnsureBoundWorkspaceCreatesOneMetadataOnlyWorkspace(t *testing.T) {
	manager := NewBrowserWorkspaceManager(t.TempDir())
	created, err := manager.EnsureBoundWorkspace("stable-account", "账号池-1")
	if err != nil || !created {
		t.Fatalf("first ensure = created:%v err:%v", created, err)
	}
	created, err = manager.EnsureBoundWorkspace("stable-account", "账号池-1")
	if err != nil || created {
		t.Fatalf("second ensure = created:%v err:%v", created, err)
	}
	items, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Bound || items[0].DebugPort != 0 || items[0].ProfileDir != "" {
		t.Fatalf("unexpected metadata-only workspace: %#v", items)
	}
}

func TestBrowserWorkspaceAccountBindingRejectsDuplicateProfiles(t *testing.T) {
	manager := NewBrowserWorkspaceManager(t.TempDir())
	items := []BrowserWorkspace{
		{ID: "one", Name: "Leonardo-01"},
		{ID: "two", Name: "Leonardo-02"},
	}
	if err := manager.save(items); err != nil {
		t.Fatal(err)
	}
	if err := manager.BindAccount("one", "stable-account"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BindAccount("two", "stable-account"); !errors.Is(err, ErrAccountWorkspaceAlreadyBound) {
		t.Fatal("expected duplicate account binding to be rejected")
	}
	got, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if got[0].AccountID != "stable-account" || got[1].AccountID != "" {
		t.Fatalf("unexpected account bindings: %#v", got)
	}
	raw, err := os.ReadFile(manager.workspaceFile())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"accountId": "stable-account"`)) {
		t.Fatal("stable account binding was not persisted")
	}
}

func TestBrowserWorkspaceGetReturnsHiddenAccountBinding(t *testing.T) {
	manager := NewBrowserWorkspaceManager(t.TempDir())
	if err := manager.save([]BrowserWorkspace{{
		ID: "one", Name: "Leonardo-01", AccountID: "stable-account",
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := manager.Get("one")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "stable-account" {
		t.Fatalf("Get().AccountID = %q", got.AccountID)
	}
	if _, err := manager.Get("missing"); err == nil {
		t.Fatal("expected missing workspace error")
	}
}

func TestFormatLeonardoCookieHeaderFiltersSortsAndKeepsHttpOnlyValues(t *testing.T) {
	raw := []any{
		map[string]any{"name": "analytics", "value": "a", "domain": ".leonardo.ai", "path": "/"},
		map[string]any{"name": "__Secure-better-auth.session_token", "value": "session", "domain": "app.leonardo.ai", "path": "/"},
		map[string]any{"name": "foreign", "value": "no", "domain": ".notleonardo.ai", "path": "/"},
		map[string]any{"name": "cf_clearance", "value": "clear", "domain": ".leonardo.ai", "path": "/"},
		map[string]any{"name": "analytics", "value": "a", "domain": ".leonardo.ai", "path": "/"},
	}
	want := "__Secure-better-auth.session_token=session; cf_clearance=clear"
	if got := formatLeonardoCookieHeader(raw); got != want {
		t.Fatalf("formatLeonardoCookieHeader() = %q, want %q", got, want)
	}
}

func TestFormatLeonardoCookieHeaderChoosesMostSpecificSameName(t *testing.T) {
	raw := []any{
		map[string]any{"name": "__Secure-better-auth.session_token", "value": "domain", "domain": ".leonardo.ai", "path": "/"},
		map[string]any{"name": "__Secure-better-auth.session_token", "value": "exact", "domain": "app.leonardo.ai", "path": "/api/auth"},
	}
	if got, want := formatLeonardoCookieHeader(raw), "__Secure-better-auth.session_token=exact"; got != want {
		t.Fatalf("formatLeonardoCookieHeader() = %q, want %q", got, want)
	}
}
