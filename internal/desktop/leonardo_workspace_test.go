package desktop

import (
	"errors"
	"testing"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

func TestDecorateLeonardoWorkspaceNamesAddsEmailSuffix(t *testing.T) {
	items := []leonardo.BrowserWorkspace{
		{ID: "one", Name: "Leonardo账号-01", AccountID: "account-one"},
		{ID: "two", Name: "Leonardo账号-02（two@example.com）", AccountID: "account-two"},
		{ID: "three", Name: "待导入", AccountID: ""},
	}
	rows := []store.Cookie{
		{AccountID: "account-one", Email: "one@example.com"},
		{AccountID: "account-two", Email: "two@example.com"},
		{AccountID: "account-unused", Email: "unused@example.com"},
	}

	got := decorateLeonardoWorkspaceNames(items, rows)
	if got[0].Name != "Leonardo账号-01（one@example.com）" {
		t.Fatalf("workspace one name = %q", got[0].Name)
	}
	if got[1].Name != "Leonardo账号-02（two@example.com）" {
		t.Fatalf("workspace two name was duplicated: %q", got[1].Name)
	}
	if got[2].Name != "待导入" {
		t.Fatalf("unbound workspace name = %q", got[2].Name)
	}
}

func TestBoundLeonardoWorkspaceNeverUsesGenericAdd(t *testing.T) {
	addCalled := false
	refreshCalled := false
	wantErr := errors.New("workspace identity changed")
	_, err := importLeonardoWorkspaceCookie(
		leonardo.BrowserWorkspace{ID: "workspace-one", AccountID: "stable-account-one"},
		"cookie=secret",
		func(string) (service.UserInfoResult, error) {
			addCalled = true
			return service.UserInfoResult{}, nil
		},
		func(accountID, raw string) (service.UserInfoResult, error) {
			refreshCalled = true
			if accountID != "stable-account-one" || raw != "cookie=secret" {
				t.Fatalf("unexpected refresh arguments: %q %q", accountID, raw)
			}
			return service.UserInfoResult{}, wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if addCalled {
		t.Fatal("generic add was called for a bound workspace")
	}
	if !refreshCalled {
		t.Fatal("bound refresh was not called")
	}
}

func TestUnboundLeonardoWorkspaceUsesGenericAdd(t *testing.T) {
	addCalled := false
	refreshCalled := false
	info, err := importLeonardoWorkspaceCookie(
		leonardo.BrowserWorkspace{ID: "workspace-new"},
		"cookie=secret",
		func(raw string) (service.UserInfoResult, error) {
			addCalled = true
			return service.UserInfoResult{AccountID: "stable-new"}, nil
		},
		func(string, string) (service.UserInfoResult, error) {
			refreshCalled = true
			return service.UserInfoResult{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !addCalled || refreshCalled || info.AccountID != "stable-new" {
		t.Fatalf("unexpected import routing: add=%v refresh=%v info=%#v", addCalled, refreshCalled, info)
	}
}
