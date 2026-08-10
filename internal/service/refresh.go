package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

// RefreshResult mirrors the Python refresh_cookie_profiles return type.
type RefreshResult struct {
	Checked   int
	OK        int
	Reenabled int
	Merged    int
}

func (p *LeonardoPool) finishCookieRefresh(res RefreshResult) (RefreshResult, error) {
	merged, err := p.store.MergeDuplicateCookieAccounts()
	if err != nil {
		return res, fmt.Errorf("service: merge duplicate Leonardo accounts: %w", err)
	}
	res.Merged = merged
	return res, nil
}

// RefreshCookieProfiles iterates every cookie (including disabled ones),
// resolves a fresh JWT, fetches the user profile, and updates email +
// balance + status.
//
// Behaviour matches Python refresh_cookie_profiles:
//   - Active cookies that fail token resolve are auto-disabled with
//     AUTH_EXPIRED. Inactive ones just get last_error updated.
//   - Active cookies with zero balance are auto-disabled with DEPLETED.
//   - Successful refreshes increment the OK counter.
func (p *LeonardoPool) RefreshCookieProfiles() (RefreshResult, error) {
	cookies, err := p.store.ListCookies()
	if err != nil {
		return RefreshResult{}, fmt.Errorf("service: list cookies: %w", err)
	}

	res := RefreshResult{}
	for _, c := range cookies {
		res.Checked++
		isActive := c.IsActive == 1

		resolvedSession := p.resolveTokenForCookieAttempt(c, false)
		token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
		if token == "" {
			if accountChanged {
				// Identity mismatch is already persisted by the session manager.
			} else if resolvedSession.Err != nil {
				_ = p.store.MarkCookieError(c.ID, resolvedSession.Err.Error())
			} else {
				_ = p.store.MarkCookieError(c.ID, "Leonardo 登录会话暂时不可用")
			}
			continue
		}

		info, err := p.api.GetUserInfo(token)
		if err != nil {
			msg := err.Error()
			if isAuthError(msg) {
				p.reportRejectedJWT(c, msg)
			}
			_ = p.store.MarkCookieError(c.ID, msg)
			continue
		}
		if err := p.ensureCookieIdentity(c, info); err != nil {
			continue
		}

		p.refreshFallbackToken(c.ID, c.Value, token)
		_ = p.store.UpdateCookieProfile(c.ID, info.ID, info.Email, info.Tokens)

		if info.Tokens <= 0 {
			if isActive || strings.EqualFold(c.DisabledReason, "AUTH_EXPIRED") {
				p.disableCookie(c.ID, "DEPLETED", "Token balance is empty")
			}
			continue
		}

		if p.reenableRecoveredAuthCookie(c, info.Tokens) {
			res.Reenabled++
		}
		_ = p.store.MarkCookieUsed(c.ID)
		res.OK++
	}

	return p.finishCookieRefresh(res)
}

// RefreshCookieSessions re-resolves the JWT for every cookie via TLS
// impersonation, similar to RefreshCookieProfiles but with stricter token
// validation. Mirrors refresh_cookie_sessions in the Python codebase.
func (p *LeonardoPool) RefreshCookieSessions() (RefreshResult, error) {
	cookies, err := p.store.ListCookies()
	if err != nil {
		return RefreshResult{}, fmt.Errorf("service: list cookies: %w", err)
	}

	res := RefreshResult{}
	for _, c := range cookies {
		res.Checked++
		isActive := c.IsActive == 1

		resolvedSession := p.resolveTokenForCookieAttempt(c, true)
		token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
		if token == "" {
			if accountChanged {
				// Already handled by the session manager.
			} else if resolvedSession.Err != nil {
				_ = p.store.MarkCookieError(c.ID, resolvedSession.Err.Error())
			} else {
				_ = p.store.MarkCookieError(c.ID, "Session refresh gagal: token tidak ditemukan")
			}
			continue
		}
		if strings.Count(token, ".") != 2 {
			msg := "Session refresh gagal: token bearer tidak valid"
			p.reportRejectedJWT(c, msg)
			_ = p.store.MarkCookieError(c.ID, msg)
			continue
		}

		info, err := p.api.GetUserInfo(token)
		if err != nil {
			msg := fmt.Sprintf("Session refresh gagal: %s", err.Error())
			if isAuthError(msg) {
				p.reportRejectedJWT(c, msg)
			}
			_ = p.store.MarkCookieError(c.ID, msg)
			continue
		}
		if err := p.ensureCookieIdentity(c, info); err != nil {
			continue
		}

		p.refreshFallbackToken(c.ID, c.Value, token)
		_ = p.store.UpdateCookieProfile(c.ID, info.ID, info.Email, info.Tokens)

		if info.Tokens <= 0 {
			if isActive || strings.EqualFold(c.DisabledReason, "AUTH_EXPIRED") {
				p.disableCookie(c.ID, "DEPLETED", "Token balance is empty")
			}
			continue
		}

		if p.reenableRecoveredAuthCookie(c, info.Tokens) {
			res.Reenabled++
		}
		_ = p.store.MarkCookieUsed(c.ID)
		res.OK++
	}

	return p.finishCookieRefresh(res)
}

// RecoverExpiredCookies periodically checks only accounts that were
// auto-disabled because authentication expired or a shared browser session
// changed account. When the stored material can again resolve a token for the
// original account identity, the account is re-enabled automatically.
func (p *LeonardoPool) RecoverExpiredCookies() (RefreshResult, error) {
	cookies, err := p.store.ListCookies()
	if err != nil {
		return RefreshResult{}, fmt.Errorf("service: list cookies: %w", err)
	}

	res := RefreshResult{}
	for _, c := range cookies {
		if c.IsActive == 1 || !isRecoverableLeonardoSessionReason(c.DisabledReason) {
			continue
		}
		res.Checked++
		resolvedSession := p.resolveTokenForCookieAttempt(c, true)
		token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
		if token == "" {
			if accountChanged {
				// Already handled by the session manager.
			} else if resolvedSession.Err != nil {
				_ = p.store.MarkCookieError(c.ID, resolvedSession.Err.Error())
			} else {
				_ = p.store.MarkCookieError(c.ID, "Leonardo 登录会话暂时不可用")
			}
			continue
		}
		info, err := p.api.GetUserInfo(token)
		if err != nil {
			_ = p.store.MarkCookieError(c.ID, err.Error())
			continue
		}
		if err := p.ensureCookieIdentity(c, info); err != nil {
			continue
		}
		p.refreshFallbackToken(c.ID, c.Value, token)
		_ = p.store.UpdateCookieProfile(c.ID, info.ID, info.Email, info.Tokens)
		if info.Tokens <= 0 {
			p.disableCookie(c.ID, "DEPLETED", "Token balance is empty")
			continue
		}
		if p.reenableRecoveredAuthCookie(c, info.Tokens) {
			res.Reenabled++
		}
		_ = p.store.MarkCookieUsed(c.ID)
		res.OK++
	}
	return p.finishCookieRefresh(res)
}

// RefreshExpiringCookieSessions proactively refreshes enabled accounts whose
// cached JWT expires within the leo2api-style margin. It also retries temporary
// failures after their backoff window without touching manually disabled,
// depleted or confirmed invalid accounts.
func (p *LeonardoPool) RefreshExpiringCookieSessions() (RefreshResult, error) {
	cookies, err := p.store.ListCookies()
	if err != nil {
		return RefreshResult{}, fmt.Errorf("service: list cookies: %w", err)
	}
	now := time.Now().Unix()
	res := RefreshResult{}
	for _, c := range cookies {
		if !shouldRefreshLeonardoSession(c, now) {
			// A transient cooldown suppresses another direct get-session call, but
			// the independent Chrome-workspace recovery must still be triggered.
			if needsLeonardoBrowserRecovery(c, now) {
				res.Checked++
			}
			continue
		}
		res.Checked++
		resolved := p.resolveTokenForCookieAttempt(c, true)
		if resolved.Token == "" {
			continue
		}
		res.OK++
		if c.IsActive == 0 {
			res.Reenabled++
		}
	}
	return res, nil
}

func needsLeonardoBrowserRecovery(c store.Cookie, now int64) bool {
	status := strings.ToLower(strings.TrimSpace(c.SessionStatus))
	if c.IsActive != 1 || strings.EqualFold(c.DisabledReason, "DEPLETED") || status == leoInvalidStatus || status == leoAbnormalStatus {
		return false
	}
	return status == leoTemporaryStatus && c.JWTExpiresAt <= now
}

func (p *LeonardoPool) reenableRecoveredAuthCookie(c store.Cookie, balance int64) bool {
	if c.IsActive == 1 || balance <= 0 || !isRecoverableLeonardoSessionReason(c.DisabledReason) {
		return false
	}
	return p.store.ToggleCookie(c.ID, true) == nil
}

func shouldRefreshLeonardoSession(c store.Cookie, now int64) bool {
	status := strings.ToLower(strings.TrimSpace(c.SessionStatus))
	if c.IsActive != 1 || strings.EqualFold(c.DisabledReason, "DEPLETED") || status == leoInvalidStatus || status == leoAbnormalStatus {
		return false
	}
	if c.ErrorUntil > now {
		return false
	}
	expiresSoon := c.JWTExpiresAt <= now+int64(leoJWTRefreshMargin.Seconds())
	keepaliveDue := c.LastRefreshAt <= 0 || c.LastRefreshAt <= now-int64(leoSessionKeepalivePeriod.Seconds())
	return expiresSoon || keepaliveDue || status == leoTemporaryStatus
}

func isRecoverableLeonardoSessionReason(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "AUTH_EXPIRED", "ACCOUNT_CHANGED", "ABNORMAL", "INVALID", "TEMPORARY_UNAVAILABLE":
		return true
	default:
		return false
	}
}
