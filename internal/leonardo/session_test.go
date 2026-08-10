package leonardo

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMergeSessionResponseCookiesRotatesAndDeduplicates(t *testing.T) {
	got := mergeSessionResponseCookies(
		"analytics=1; __Secure-better-auth.session_token=old; analytics=2",
		[]*http.Cookie{
			{Name: "__Secure-better-auth.session_token", Value: "new"},
			{Name: "__Secure-better-auth.session_data.0", Value: "part0"},
		},
	)
	if strings.Count(got, "analytics=") != 1 {
		t.Fatalf("duplicate cookie name was retained: %q", got)
	}
	if !strings.Contains(got, "__Secure-better-auth.session_token=new") || !strings.Contains(got, "__Secure-better-auth.session_data.0=part0") {
		t.Fatalf("rotation was not merged: %q", got)
	}
}

func TestCriticalCookieDeletionDetectionUsesFinalAction(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	if deletesCriticalSessionCookie([]*http.Cookie{
		{Name: "__Secure-better-auth.session_token", Value: "", Expires: past},
		{Name: "__Secure-better-auth.session_token", Value: "replacement"},
	}) {
		t.Fatal("final replacement must override an earlier deletion")
	}
	if !deletesCriticalSessionCookie([]*http.Cookie{{Name: "__Secure-better-auth.session_token", Expires: past}}) {
		t.Fatal("critical deletion was not detected")
	}
}

func TestClassifySessionHTTPError(t *testing.T) {
	tests := []struct {
		status int
		body   string
		kind   SessionErrorKind
	}{
		{401, "unauthorized", SessionErrorAuth},
		{429, "rate limited", SessionErrorRateLimit},
		{503, "upstream unavailable", SessionErrorTransient},
		{403, "<!doctype html><title>Vercel Challenge</title>", SessionErrorChallenge},
		{403, "forbidden", SessionErrorChallenge},
	}
	for _, tt := range tests {
		if got := classifySessionHTTPError(tt.status, []byte(tt.body)); got.Kind != tt.kind {
			t.Fatalf("status %d kind=%s want=%s", tt.status, got.Kind, tt.kind)
		}
	}
}

func TestIncompleteSessionResponsesRemainRetryable(t *testing.T) {
	for _, kind := range []SessionErrorKind{SessionErrorNoJWT, SessionErrorInvalid} {
		err := &SessionRefreshError{Kind: kind, Message: string(kind)}
		if !err.Temporary() {
			t.Fatalf("session error %s should be retryable", kind)
		}
	}
	if (&SessionRefreshError{Kind: SessionErrorAuth, Message: "unauthorized"}).Temporary() {
		t.Fatal("confirmed authentication failure must not be classified as temporary")
	}
}
