package service

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

func TestReenableRecoveredAuthCookie(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AddCookie("token=test"); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListCookies()
	if err != nil || len(rows) != 1 {
		t.Fatalf("cookies=%d err=%v", len(rows), err)
	}
	if err := st.AutoDisableCookie(rows[0].ID, "AUTH_EXPIRED"); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.ListCookies()
	pool := &LeonardoPool{store: st}
	if !pool.reenableRecoveredAuthCookie(rows[0], 100) {
		t.Fatal("expected account to be re-enabled")
	}
	rows, _ = st.ListCookies()
	if rows[0].IsActive != 1 || rows[0].DisabledReason != "" {
		t.Fatalf("active=%d reason=%q", rows[0].IsActive, rows[0].DisabledReason)
	}
}

func TestRecoverableLeonardoSessionReasons(t *testing.T) {
	for _, reason := range []string{"AUTH_EXPIRED", "ACCOUNT_CHANGED", "ABNORMAL", "INVALID", "TEMPORARY_UNAVAILABLE"} {
		if !isRecoverableLeonardoSessionReason(reason) {
			t.Fatalf("reason %q should be recoverable", reason)
		}
	}
	for _, reason := range []string{"", "DEPLETED", "MANUAL_DISABLED"} {
		if isRecoverableLeonardoSessionReason(reason) {
			t.Fatalf("reason %q should not be recovered automatically", reason)
		}
	}
}

func TestShouldRefreshLeonardoSessionBeforeHourlyJWTExpires(t *testing.T) {
	now := time.Now().Unix()
	base := store.Cookie{
		IsActive:      1,
		SessionStatus: "active",
		JWTExpiresAt:  now + int64((40 * time.Minute).Seconds()),
		LastRefreshAt: now,
	}
	if shouldRefreshLeonardoSession(base, now) {
		t.Fatal("fresh token and recent session refresh should stay cached")
	}

	nearExpiry := base
	nearExpiry.JWTExpiresAt = now + int64((14 * time.Minute).Seconds())
	if !shouldRefreshLeonardoSession(nearExpiry, now) {
		t.Fatal("JWT inside the 15-minute renewal window should refresh")
	}

	keepaliveDue := base
	keepaliveDue.JWTExpiresAt = now + int64((2 * time.Hour).Seconds())
	keepaliveDue.LastRefreshAt = now - int64((31 * time.Minute).Seconds())
	if !shouldRefreshLeonardoSession(keepaliveDue, now) {
		t.Fatal("long-lived token should still refresh the browser session every 30 minutes")
	}

	coolingDown := nearExpiry
	coolingDown.ErrorUntil = now + 60
	if shouldRefreshLeonardoSession(coolingDown, now) {
		t.Fatal("refresh backoff must be respected")
	}
}

func TestTemporaryCooldownStillRequestsBrowserRecovery(t *testing.T) {
	now := time.Now().Unix()
	cookie := store.Cookie{
		IsActive:      1,
		SessionStatus: leoTemporaryStatus,
		JWTExpiresAt:  now - 60,
		ErrorUntil:    now + int64((30 * time.Minute).Seconds()),
	}
	if shouldRefreshLeonardoSession(cookie, now) {
		t.Fatal("direct refresh should respect the error backoff")
	}
	if !needsLeonardoBrowserRecovery(cookie, now) {
		t.Fatal("expired temporary session should still trigger Chrome recovery")
	}
}

func TestValidJWTStaysSchedulableDuringRefreshCooldown(t *testing.T) {
	now := time.Now()
	jwt := testLeonardoJWT(t, now.Add(20*time.Minute).Unix())
	pool := &LeonardoPool{}
	cookie := store.Cookie{
		IsActive:      1,
		SessionStatus: leoActiveStatus,
		JWTToken:      jwt,
		JWTExpiresAt:  now.Add(20 * time.Minute).Unix(),
		ErrorUntil:    now.Add(30 * time.Minute).Unix(),
		LastError:     "rate limited",
	}
	if pool.shouldSkipCookieNow(cookie) {
		t.Fatal("a still-valid cached JWT must remain available for generation")
	}
	cookie.JWTToken = ""
	cookie.JWTExpiresAt = now.Add(-time.Minute).Unix()
	if !pool.shouldSkipCookieNow(cookie) {
		t.Fatal("an expired session must respect refresh cooldown")
	}
}

func TestChangedRefreshFailureReasonUsesFirstFailureCooldown(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AddCookie("__Secure-better-auth.session_token=old"); err != nil {
		t.Fatal(err)
	}
	row, _ := st.GetCookieByValue("__Secure-better-auth.session_token=old")
	for i := 0; i < 4; i++ {
		if _, err := st.RecordCookieRefreshFailure(row.ID, "session response missing JWT", time.Now().Add(time.Minute).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	row, _ = st.GetCookieByID(row.ID)
	pool := &LeonardoPool{store: st}
	temporary := &leonardo.SessionRefreshError{Kind: leonardo.SessionErrorTransient, Message: "timeout"}
	count, _ := pool.recordSessionRefreshFailureOnly(*row, temporary, true, "")
	if count != 1 {
		t.Fatalf("changed reason should reset count to 1, got %d", count)
	}
	updated, _ := st.GetCookieByID(row.ID)
	remaining := time.Until(time.Unix(updated.ErrorUntil, 0))
	if remaining > 90*time.Second || remaining < 30*time.Second {
		t.Fatalf("first temporary failure should cool down for about one minute, got %s", remaining)
	}
}

func testLeonardoJWT(t *testing.T, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
	return header + "." + payload + ".signature"
}
