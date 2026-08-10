package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

const (
	// Leonardo's browser JWT normally lasts about one hour. Refreshing only
	// five minutes before expiry leaves too little room for a proxy hiccup,
	// security checkpoint or rate limit. Start renewal earlier and periodically
	// touch the Better Auth session so rotated cookies are persisted well before
	// the short-lived JWT expires.
	leoJWTRefreshMargin       = 15 * time.Minute
	leoSessionKeepalivePeriod = 30 * time.Minute
	leoRefreshFailLimit       = 2
	leoTemporaryStatus        = "temporary_unavailable"
	leoInvalidStatus          = "invalid"
	leoAbnormalStatus         = "abnormal"
	leoActiveStatus           = "active"
)

type managedLeonardoSession struct {
	mu sync.Mutex
}

type tokenResolveResult struct {
	Token          string
	AccountChanged bool
	Temporary      bool
	ConfirmedDead  bool
	Err            error
}

func (p *LeonardoPool) managedSession(id int64) *managedLeonardoSession {
	p.sessionsMu.Lock()
	defer p.sessionsMu.Unlock()
	if p.sessions == nil {
		p.sessions = make(map[int64]*managedLeonardoSession)
	}
	if session := p.sessions[id]; session != nil {
		return session
	}
	session := &managedLeonardoSession{}
	p.sessions[id] = session
	return session
}

// resolveTokenForCookieAttempt is the replacement account-pool session core.
// force=true bypasses the cached JWT and performs one serialized get-session
// refresh. All callers for the same account share the same refresh lock.
func (p *LeonardoPool) resolveTokenForCookieAttempt(cookie store.Cookie, force bool) tokenResolveResult {
	if p == nil || p.api == nil || p.store == nil {
		return tokenResolveResult{Err: errors.New("Leonardo 账号池尚未初始化")}
	}
	session := p.managedSession(cookie.ID)
	session.mu.Lock()
	defer session.mu.Unlock()

	if current, err := p.store.GetCookieByID(cookie.ID); err == nil && current != nil {
		cookie = *current
	}
	expected := strings.TrimSpace(cookie.AccountID)
	parsedToken, cookiePayload := extractAuthParts(cookie.Value)
	if strings.TrimSpace(cookie.JWTToken) != "" {
		parsedToken = strings.TrimSpace(cookie.JWTToken)
	}
	cookiePayload = normalizeStoredCookiePayload(cookiePayload, cookie.Value)

	accept := func(candidate string, margin int64) (string, bool) {
		candidate = strings.TrimSpace(candidate)
		if !leonardo.IsFreshToken(candidate, margin) || !leonardo.LooksLikeJWT(candidate) {
			return "", false
		}
		resolved := strings.TrimSpace(leonardo.UserIDFromToken(candidate))
		if expected == "" || resolved == "" || resolved == expected {
			return candidate, false
		}
		return "", true
	}

	if !force {
		if candidate, mismatch := accept(parsedToken, int64(leoJWTRefreshMargin.Seconds())); candidate != "" {
			return tokenResolveResult{Token: candidate}
		} else if mismatch {
			p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
			return tokenResolveResult{AccountChanged: true, ConfirmedDead: true, Err: errors.New(accountChangedMessage)}
		}
		if cookie.ErrorUntil > time.Now().Unix() {
			return tokenResolveResult{Temporary: true, Err: fmt.Errorf("账号刷新冷却至 %s", time.Unix(cookie.ErrorUntil, 0).Format("15:04:05"))}
		}
	}

	if cookiePayload == "" {
		if candidate, mismatch := accept(parsedToken, 0); candidate != "" {
			return tokenResolveResult{Token: candidate}
		} else if mismatch {
			p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
			return tokenResolveResult{AccountChanged: true, ConfirmedDead: true, Err: errors.New(accountChangedMessage)}
		}
		refreshErr := &leonardo.SessionRefreshError{Kind: leonardo.SessionErrorAuth, Message: "账号没有可续期的 Leonardo Session Cookie"}
		return p.recordSessionRefreshFailure(cookie, refreshErr, parsedToken)
	}

	leaseOwner := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	acquired, leaseErr := p.store.TryAcquireCookieRefreshLease(cookie.ID, leaseOwner, time.Now().Add(90*time.Second).Unix())
	if leaseErr != nil {
		return tokenResolveResult{Temporary: true, Err: fmt.Errorf("获取账号刷新锁失败: %w", leaseErr)}
	}
	if !acquired {
		// Another local process is already refreshing this account. Wait briefly
		// for it to publish the new JWT instead of making a duplicate request.
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			current, loadErr := p.store.GetCookieByID(cookie.ID)
			if loadErr != nil || current == nil {
				continue
			}
			if candidate, mismatch := accept(current.JWTToken, 0); candidate != "" {
				return tokenResolveResult{Token: candidate}
			} else if mismatch {
				p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
				return tokenResolveResult{AccountChanged: true, ConfirmedDead: true, Err: errors.New(accountChangedMessage)}
			}
			if current.RefreshLease <= time.Now().Unix() {
				break
			}
		}
		acquired, leaseErr = p.store.TryAcquireCookieRefreshLease(cookie.ID, leaseOwner, time.Now().Add(90*time.Second).Unix())
		if leaseErr != nil || !acquired {
			return tokenResolveResult{Temporary: true, Err: errors.New("该账号正在由另一个本地进程刷新，请稍后重试")}
		}
	}
	defer p.store.ReleaseCookieRefreshLease(cookie.ID, leaseOwner)

	refreshed, err := p.api.RefreshSession(cookiePayload)
	// Persist safe, non-destructive rotations even when this response omitted a
	// JWT. The next refresh then uses Leonardo's newest session_token/data.
	if err != nil && refreshed.Rotated && strings.TrimSpace(refreshed.Cookie) != "" {
		_, _ = p.store.UpdateCookieValue(cookie.ID, refreshed.Cookie)
		cookiePayload = refreshed.Cookie
	}
	if err != nil {
		// A proactive refresh failure does not discard a JWT that still works.
		if candidate, mismatch := accept(parsedToken, 0); candidate != "" {
			_, _ = p.recordSessionRefreshFailureOnly(cookie, err, false, parsedToken)
			return tokenResolveResult{Token: candidate, Temporary: isTemporarySessionError(err), Err: err}
		} else if mismatch {
			p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
			return tokenResolveResult{AccountChanged: true, ConfirmedDead: true, Err: errors.New(accountChangedMessage)}
		}
		return p.recordSessionRefreshFailure(cookie, err, parsedToken)
	}

	if expected != "" && strings.TrimSpace(refreshed.AccountID) != "" && strings.TrimSpace(refreshed.AccountID) != expected {
		p.disableCookie(cookie.ID, "ACCOUNT_CHANGED", accountChangedMessage)
		return tokenResolveResult{AccountChanged: true, ConfirmedDead: true, Err: errors.New(accountChangedMessage)}
	}
	if strings.TrimSpace(refreshed.Cookie) == "" {
		refreshed.Cookie = cookiePayload
	}
	_, _ = p.store.UpdateCookieValue(cookie.ID, refreshed.Cookie)
	_ = p.store.UpdateCookieSessionSuccess(cookie.ID, refreshed.Token, refreshed.ExpiresAt)
	return tokenResolveResult{Token: refreshed.Token}
}

func normalizeStoredCookiePayload(parsedCookie, raw string) string {
	cookiePayload := strings.TrimSpace(parsedCookie)
	if strings.HasPrefix(strings.ToLower(cookiePayload), "cookie:") {
		cookiePayload = strings.TrimSpace(cookiePayload[len("cookie:"):])
	}
	if cookiePayload == "" {
		candidate := strings.TrimSpace(raw)
		if strings.Contains(candidate, ";") && strings.Contains(candidate, "=") && !strings.HasPrefix(strings.ToLower(candidate), "token=") {
			cookiePayload = candidate
		}
	}
	return cookiePayload
}

func isTemporarySessionError(err error) bool {
	var sessionErr *leonardo.SessionRefreshError
	return errors.As(err, &sessionErr) && sessionErr.Temporary()
}

func normalizeSessionRefreshReason(err error) string {
	var sessionErr *leonardo.SessionRefreshError
	if errors.As(err, &sessionErr) {
		switch sessionErr.Kind {
		case leonardo.SessionErrorRateLimit:
			return "rate limited"
		case leonardo.SessionErrorChallenge:
			return "upstream challenge"
		case leonardo.SessionErrorTransient:
			return "temporary network/upstream error"
		case leonardo.SessionErrorAuth:
			if sessionErr.StatusCode == 403 {
				return "http 403 forbidden"
			}
			return "http 401 unauthorized"
		case leonardo.SessionErrorNoJWT:
			return "session response missing JWT"
		case leonardo.SessionErrorInvalid:
			return "invalid session response"
		}
	}
	reason := strings.TrimSpace(err.Error())
	if len(reason) > 160 {
		reason = reason[:160]
	}
	return reason
}

func (p *LeonardoPool) recordSessionRefreshFailure(cookie store.Cookie, err error, oldToken string) tokenResolveResult {
	count, status := p.recordSessionRefreshFailureOnly(cookie, err, true, oldToken)
	confirmed := status == leoInvalidStatus || status == leoAbnormalStatus
	return tokenResolveResult{
		Temporary:     isTemporarySessionError(err),
		ConfirmedDead: confirmed,
		Err:           fmt.Errorf("%s（连续失败 %d 次）", err.Error(), count),
	}
}

func (p *LeonardoPool) recordSessionRefreshFailureOnly(cookie store.Cookie, err error, allowDisable bool, currentToken string) (int, string) {
	reason := normalizeSessionRefreshReason(err)
	// RecordCookieRefreshFailure resets the counter when the normalized reason
	// changes. Calculate the delay from that same prospective count; using the
	// old row's count caused a first timeout after several no-JWT failures to be
	// displayed as "1 failure" while still receiving a 30-minute cooldown.
	nextCount := 1
	if strings.EqualFold(strings.TrimSpace(cookie.RefreshReason), reason) {
		nextCount = cookie.RefreshFails + 1
	}
	delay := time.Minute
	if !isTemporarySessionError(err) {
		delays := []time.Duration{time.Minute, 3 * time.Minute, 10 * time.Minute, 30 * time.Minute}
		idx := nextCount - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(delays) {
			idx = len(delays) - 1
		}
		delay = delays[idx]
	}
	errorUntil := time.Now().Add(delay).Unix()
	count, storeErr := p.store.RecordCookieRefreshFailure(cookie.ID, reason, errorUntil)
	if storeErr != nil {
		return 0, ""
	}

	var sessionErr *leonardo.SessionRefreshError
	_ = errors.As(err, &sessionErr)
	if sessionErr != nil && sessionErr.Temporary() {
		status := leoActiveStatus
		if !leonardo.IsFreshToken(currentToken, 0) && !leonardo.IsFreshToken(cookie.JWTToken, 0) {
			status = leoTemporaryStatus
			_ = p.store.SetCookieSessionState(cookie.ID, status, reason, false)
		}
		return count, status
	}
	if !allowDisable || count < leoRefreshFailLimit {
		return count, leoActiveStatus
	}
	status := leoAbnormalStatus
	if sessionErr != nil && sessionErr.Kind == leonardo.SessionErrorAuth {
		status = leoInvalidStatus
	}
	_ = p.store.SetCookieSessionState(cookie.ID, status, reason, true)
	return count, status
}

// reportRejectedJWT is called after Leonardo GraphQL explicitly rejects a JWT.
// It clears the cache and requires repeated confirmed failures before disabling
// the browser session.
func (p *LeonardoPool) reportRejectedJWT(cookie store.Cookie, message string) {
	_ = p.store.ClearCookieSessionJWT(cookie.ID)
	if _, cookiePayload := extractAuthParts(cookie.Value); strings.TrimSpace(cookiePayload) != "" {
		_, _ = p.store.UpdateCookieValue(cookie.ID, normalizeStoredCookiePayload(cookiePayload, cookie.Value))
	}
	err := &leonardo.SessionRefreshError{Kind: leonardo.SessionErrorAuth, StatusCode: 401, Message: message}
	p.recordSessionRefreshFailureOnly(cookie, err, true, "")
}
