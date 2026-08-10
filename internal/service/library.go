package service

import (
	"fmt"
	"strings"
)

// LibrarySyncResult summarises one manual/automatic remote material sync.
type LibrarySyncResult struct {
	Accounts       int      `json:"accounts"`
	SyncedAccounts int      `json:"synced_accounts"`
	RemoteItems    int      `json:"remote_items"`
	Added          int      `json:"added"`
	Updated        int      `json:"updated"`
	Skipped        int      `json:"skipped"`
	Failed         int      `json:"failed"`
	Errors         []string `json:"errors"`
}

// SyncLibrary imports recent image and video generations created directly on
// Leonardo's website. It uses provider generation ids for idempotency and
// never un-hides a material the user deleted locally.
func (p *LeonardoPool) SyncLibrary(limit int) (*LibrarySyncResult, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	// Historical web generations belong to every account in the pool, not
	// only the currently enabled/credit-bearing accounts. Try every stored
	// account and report expired sessions individually.
	cookies, err := p.store.ListCookies()
	if err != nil {
		return nil, fmt.Errorf("service: list cookies: %w", err)
	}
	if len(cookies) == 0 {
		return nil, newPublicError(400, "账号池中没有可用于同步的账号")
	}

	result := &LibrarySyncResult{Accounts: len(cookies), Errors: []string{}}
	seenUsers := map[string]struct{}{}
	for _, cookie := range cookies {
		resolvedSession := p.resolveTokenForCookieAttempt(cookie, false)
		token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
		if token == "" {
			result.Failed++
			if accountChanged {
				result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：%s", cookie.ID, accountChangedMessage))
			} else if resolvedSession.Err != nil {
				result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：%s", cookie.ID, safeSyncError(resolvedSession.Err)))
			} else {
				result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：登录会话暂时不可用", cookie.ID))
			}
			continue
		}
		info, err := p.api.GetUserInfo(token)
		if err != nil {
			result.Failed++
			result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：%s", cookie.ID, safeSyncError(err)))
			continue
		}
		if err := p.ensureCookieIdentity(cookie, info); err != nil {
			result.Failed++
			result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：%s", cookie.ID, err.Error()))
			continue
		}
		_ = p.store.UpdateCookieProfile(cookie.ID, info.ID, info.Email, info.Tokens)
		_ = p.reenableRecoveredAuthCookie(cookie, info.Tokens)
		if strings.TrimSpace(info.ID) == "" {
			result.Failed++
			result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：无法识别 Leonardo 用户 ID", cookie.ID))
			continue
		}
		if _, duplicate := seenUsers[info.ID]; duplicate {
			result.Skipped++
			continue
		}
		seenUsers[info.ID] = struct{}{}

		remote, err := p.api.ListRecentGenerations(token, info.ID, limit)
		if err != nil {
			result.Failed++
			result.Errors = appendLimited(result.Errors, fmt.Sprintf("账号 #%d：%s", cookie.ID, safeSyncError(err)))
			continue
		}
		result.SyncedAccounts++
		result.RemoteItems += len(remote)
		for _, item := range remote {
			if len(item.AssetURLs) == 0 || !isLibraryReady(item.Status) {
				result.Skipped++
				continue
			}
			modelID := strings.TrimSpace(item.ModelID)
			if modelID == "" {
				modelID = "Leonardo 网页生成"
			}
			added, updated, err := p.store.UpsertGenerationLogFromRemote(
				item.ID,
				cookie.ID,
				modelID,
				aspectFromDimensions(item.ImageWidth, item.ImageHeight),
				item.Prompt,
				item.AssetURLs,
				libraryStatus(item.Status),
				item.CreatedAt,
			)
			if err != nil {
				result.Failed++
				result.Errors = appendLimited(result.Errors, fmt.Sprintf("作品 %s：保存失败", shortID(item.ID)))
				continue
			}
			switch {
			case added:
				result.Added++
			case updated:
				result.Updated++
			default:
				result.Skipped++
			}
		}
	}
	return result, nil
}

func isLibraryReady(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETE", "COMPLETED", "SUCCESS", "SUCCEEDED":
		return true
	default:
		return false
	}
}

func libraryStatus(status string) string {
	if isLibraryReady(status) {
		return "success"
	}
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FAILED", "ERROR":
		return "failed"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func aspectFromDimensions(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	divisor := gcd(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	if a == 0 {
		return 1
	}
	return a
}

func safeSyncError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return msg
}

func appendLimited(items []string, value string) []string {
	if len(items) >= 6 {
		return items
	}
	return append(items, value)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
