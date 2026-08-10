package service

import (
	"fmt"
	"log"
	"strings"
)

// UploadLocalImage uploads raw bytes (e.g. drag-drop file) and returns the
// init image id for use as a reference / start frame. It rotates through the
// active cookie pool exactly like Generate does so the upload is always
// signed with a working JWT.
func (p *LeonardoPool) UploadLocalImage(content []byte, ext string) (string, error) {
	if len(content) == 0 {
		return "", fmt.Errorf("图片内容为空")
	}
	cookies, err := p.store.ListActiveCookies()
	if err != nil {
		return "", err
	}
	if len(cookies) == 0 {
		return "", newPublicError(400, "账号池中没有可用账号")
	}

	var errs []string
	for _, cookie := range cookies {
		if p.shouldSkipCookieNow(cookie) {
			errs = append(errs, fmt.Sprintf("账号#%d 正处于登录失效冷却期", cookie.ID))
			continue
		}

		// Uploads must use the same token refresh and identity checks as image
		// and video generation. A Leonardo access token is short lived, while
		// the stored browser Cookie is expected to mint a fresh token. Retry the
		// resolution once for transient proxy/session endpoint failures before
		// disabling a genuinely expired login.
		for attempt := 0; attempt < 2; attempt++ {
			resolvedSession := p.resolveTokenForCookieAttempt(cookie, attempt > 0)
			token, accountChanged := resolvedSession.Token, resolvedSession.AccountChanged
			if token == "" {
				if accountChanged {
					errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, accountChangedMessage))
					break
				}
				if resolvedSession.Err != nil {
					errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, resolvedSession.Err.Error()))
				} else {
					errs = append(errs, fmt.Sprintf("账号#%d：登录会话暂时不可用", cookie.ID))
				}
				break
			}

			info, err := p.api.GetUserInfo(token)
			if err != nil {
				msg := err.Error()
				if isAuthError(msg) && attempt == 0 {
					p.reportRejectedJWT(cookie, msg)
					continue
				}
				if isAuthError(msg) {
					p.reportRejectedJWT(cookie, msg)
				}
				errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, msg))
				break
			}
			if err := p.ensureCookieIdentity(cookie, info); err != nil {
				errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, err.Error()))
				break
			}
			p.refreshFallbackToken(cookie.ID, cookie.Value, token)
			_ = p.store.UpdateCookieProfile(cookie.ID, info.ID, info.Email, info.Tokens)
			if info.Tokens <= 0 {
				msg := "账号积分已用完"
				p.disableCookie(cookie.ID, "DEPLETED", msg)
				errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, msg))
				break
			}

			log.Printf("[upload] cookie#%d: starting Leonardo upload (%d bytes, ext=%s)", cookie.ID, len(content), ext)
			id, err := p.api.UploadImageBytes(token, content, ext)
			if err != nil {
				msg := err.Error()
				log.Printf("[upload] cookie#%d: failed: %s", cookie.ID, msg)
				if isAuthError(msg) && attempt == 0 {
					p.reportRejectedJWT(cookie, msg)
					continue
				}
				if isAuthError(msg) {
					p.reportRejectedJWT(cookie, msg)
				}
				errs = append(errs, fmt.Sprintf("账号#%d：%s", cookie.ID, msg))
				break
			}
			_ = p.store.MarkCookieUsed(cookie.ID)
			log.Printf("[upload] cookie#%d: success id=%s", cookie.ID, id)
			return id, nil
		}
	}

	if len(errs) == 0 {
		errs = append(errs, "账号池中没有可用于上传的登录会话")
	}
	if len(errs) > 6 {
		errs = errs[:6]
	}
	return "", newPublicError(503, "上传失败：所有 Leonardo 账号均不可用。"+strings.Join(errs, "；"))
}
