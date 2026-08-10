package leonardo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// SessionErrorKind classifies get-session failures so the account pool can
// distinguish a dead login from an upstream/network problem.
type SessionErrorKind string

const (
	SessionErrorAuth      SessionErrorKind = "auth"
	SessionErrorRateLimit SessionErrorKind = "rate_limited"
	SessionErrorChallenge SessionErrorKind = "challenge"
	SessionErrorTransient SessionErrorKind = "transient"
	SessionErrorNoJWT     SessionErrorKind = "no_jwt"
	SessionErrorInvalid   SessionErrorKind = "invalid_response"
)

// SessionRefreshError is intentionally structured. Callers must not turn a
// timeout, 429, Vercel/Cloudflare challenge or upstream 5xx into AUTH_EXPIRED.
type SessionRefreshError struct {
	Kind       SessionErrorKind
	StatusCode int
	Message    string
}

func (e *SessionRefreshError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *SessionRefreshError) Temporary() bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case SessionErrorRateLimit, SessionErrorChallenge, SessionErrorTransient,
		SessionErrorNoJWT, SessionErrorInvalid:
		return true
	default:
		return false
	}
}

// SessionRefreshResult contains the current browser Cookie plus the short-lived
// JWT minted from it. Cookie is returned even when JWT extraction fails, because
// Better Auth can rotate session cookies in a response that temporarily omits a
// token. Critical cookie deletions are only accepted when a valid JWT confirms
// the response.
type SessionRefreshResult struct {
	Cookie      string
	Token       string
	ExpiresAt   int64
	AccountID   string
	Email       string
	Rotated     bool
	StatusCode  int
	ResponseKey []string
}

// RefreshSession implements the reliable Better Auth refresh behaviour used by
// leo2api: serialize persistence around the caller, merge Set-Cookie rotations,
// protect critical cookies from unconfirmed deletion and return structured
// failure categories.
func (c *Client) RefreshSession(cookieStr string) (SessionRefreshResult, error) {
	cookieStr = strings.TrimSpace(cookieStr)
	result := SessionRefreshResult{Cookie: cookieStr}
	if cookieStr == "" {
		return result, &SessionRefreshError{Kind: SessionErrorAuth, Message: "Leonardo Cookie 为空"}
	}

	resp, err := c.impersonatorClient.R().
		SetHeader("cookie", cookieStr).
		SetHeader("accept", "application/json").
		SetHeader("accept-language", "en-US,en;q=0.9").
		SetHeader("origin", "https://app.leonardo.ai").
		SetHeader("referer", "https://app.leonardo.ai/").
		Get("https://app.leonardo.ai/api/auth/get-session")
	if err != nil {
		return result, &SessionRefreshError{Kind: SessionErrorTransient, Message: "Leonardo get-session 请求失败: " + err.Error()}
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return result, &SessionRefreshError{Kind: SessionErrorTransient, StatusCode: resp.StatusCode, Message: "读取 Leonardo get-session 响应失败: " + readErr.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return result, classifySessionHTTPError(resp.StatusCode, body)
	}

	responseCookies := resp.Cookies()
	updatedCookie := mergeSessionResponseCookies(cookieStr, responseCookies)
	criticalDeletion := updatedCookie != cookieStr && deletesCriticalSessionCookie(responseCookies)
	if updatedCookie != cookieStr && !criticalDeletion {
		result.Cookie = updatedCookie
		result.Rotated = true
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return result, &SessionRefreshError{Kind: SessionErrorInvalid, StatusCode: resp.StatusCode, Message: "Leonardo get-session 返回了无效 JSON: " + err.Error()}
	}
	result.ResponseKey = mapKeysSorted(payload)
	result.Token = findTokenInObject(payload)
	if result.Token == "" || !LooksLikeJWT(result.Token) {
		if criticalDeletion {
			result.Cookie = cookieStr
			result.Rotated = false
		}
		return result, &SessionRefreshError{
			Kind:       SessionErrorNoJWT,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("Leonardo get-session 未返回 JWT，响应字段: %v", result.ResponseKey),
		}
	}

	result.ExpiresAt = TokenExp(result.Token)
	if result.ExpiresAt <= time.Now().Unix() {
		return result, &SessionRefreshError{Kind: SessionErrorAuth, StatusCode: resp.StatusCode, Message: "Leonardo get-session 返回了已过期 JWT"}
	}
	if criticalDeletion {
		result.Cookie = updatedCookie
		result.Rotated = updatedCookie != cookieStr
	}
	result.AccountID = UserIDFromToken(result.Token)
	if payload := DecodeJWTPayload(result.Token); payload != nil {
		result.Email, _ = payload["email"].(string)
	}
	return result, nil
}

func classifySessionHTTPError(status int, body []byte) *SessionRefreshError {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	lower := strings.ToLower(snippet)
	kind := SessionErrorTransient
	switch {
	case strings.Contains(lower, "vercel challenge"), strings.Contains(lower, "cloudflare challenge"),
		strings.Contains(lower, "x-vercel-challenge"), strings.HasPrefix(lower, "<!doctype html"), strings.HasPrefix(lower, "<html"):
		kind = SessionErrorChallenge
	case status == http.StatusUnauthorized:
		kind = SessionErrorAuth
	case status == http.StatusForbidden:
		// get-session 403 responses are usually Vercel/Cloudflare checkpoints,
		// proxy reputation blocks or browser-fingerprint mismatches rather than
		// proof that the stored Better Auth session has expired. Treat them as a
		// retryable challenge so one brief upstream block cannot disable an
		// otherwise long-lived account.
		kind = SessionErrorChallenge
	case status == http.StatusTooManyRequests:
		kind = SessionErrorRateLimit
	case status >= 500:
		kind = SessionErrorTransient
	default:
		kind = SessionErrorInvalid
	}
	if snippet == "" {
		snippet = http.StatusText(status)
	}
	return &SessionRefreshError{Kind: kind, StatusCode: status, Message: fmt.Sprintf("Leonardo get-session HTTP %d: %s", status, snippet)}
}

func mergeSessionResponseCookies(existing string, responseCookies []*http.Cookie) string {
	if len(responseCookies) == 0 {
		return existing
	}
	values := make([]string, 0, 16)
	index := make(map[string]int)
	for _, part := range strings.Split(existing, ";") {
		part = strings.TrimSpace(part)
		name, _, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if part == "" || !ok || name == "" {
			continue
		}
		if pos, exists := index[name]; exists {
			values[pos] = part
			continue
		}
		index[name] = len(values)
		values = append(values, part)
	}
	for _, cookie := range responseCookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		name := strings.TrimSpace(cookie.Name)
		if isSessionCookieDeletion(cookie) {
			if pos, ok := index[name]; ok {
				values = append(values[:pos], values[pos+1:]...)
				delete(index, name)
				for i := pos; i < len(values); i++ {
					cookieName, _, _ := strings.Cut(values[i], "=")
					index[strings.TrimSpace(cookieName)] = i
				}
			}
			continue
		}
		value := name + "=" + cookie.Value
		if pos, ok := index[name]; ok {
			values[pos] = value
		} else {
			index[name] = len(values)
			values = append(values, value)
		}
	}
	return strings.Join(values, "; ")
}

func isSessionCookieDeletion(cookie *http.Cookie) bool {
	return cookie != nil && (cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now())))
}

func deletesCriticalSessionCookie(cookies []*http.Cookie) bool {
	finalDeletion := make(map[string]bool)
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		name := strings.TrimSpace(cookie.Name)
		if name == "__Secure-better-auth.session_token" || strings.HasPrefix(name, "__Secure-better-auth.session_data.") {
			finalDeletion[name] = isSessionCookieDeletion(cookie)
		}
	}
	for _, deleted := range finalDeletion {
		if deleted {
			return true
		}
	}
	return false
}

func mapKeysSorted(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
