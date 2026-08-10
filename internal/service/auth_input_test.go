package service

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

func TestExtractAuthPartsAcceptsCopiedCurl(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantToken  string
		wantCookie string
	}{
		{
			name:       "bash single quotes",
			raw:        "curl 'https://app.leonardo.ai/' -H 'accept: */*' -H 'cookie: session=abc; preference=zh-CN'",
			wantCookie: "session=abc; preference=zh-CN",
		},
		{
			name:       "windows double quotes",
			raw:        "curl \"https://app.leonardo.ai/\" ^\r\n  -H \"cookie: session=abc; auth=value\" ^\r\n  -H \"accept: */*\"",
			wantCookie: "session=abc; auth=value",
		},
		{
			name: "chrome curl uses cookie flag and bearer header",
			raw: "curl 'https://api.leonardo.ai/v1/graphql' \\\n" +
				"  -H 'authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.signature' \\\n" +
				"  -b '__Secure-better-auth.session_token=opaque; preference=zh-CN'",
			wantToken:  "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.signature",
			wantCookie: "__Secure-better-auth.session_token=opaque; preference=zh-CN",
		},
		{
			name:       "long cookie option",
			raw:        "curl.exe https://api.leonardo.ai/v1/graphql --cookie=\"better-auth.session_token=opaque\" --header=\"accept: application/json\"",
			wantCookie: "better-auth.session_token=opaque",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, cookie := extractAuthParts(tt.raw)
			if token != tt.wantToken {
				t.Fatalf("token = %q, want %q", token, tt.wantToken)
			}
			if cookie != tt.wantCookie {
				t.Fatalf("cookie = %q, want %q", cookie, tt.wantCookie)
			}
		})
	}
}

func TestComposeStoreAuthValueAllowsValidatedCurlTokenWithoutCookie(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.signature"
	if got, want := composeStoreAuthValue("", token), "token="+token; got != want {
		t.Fatalf("composeStoreAuthValue = %q, want %q", got, want)
	}
}

func TestResolveTokenForCookiePrefersFallbackMatchingStoredAccount(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"exp":                          time.Now().Add(time.Hour).Unix(),
		"https://hasura.io/jwt/claims": `{"x-hasura-user-id":"account-1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	pool := &LeonardoPool{}
	got, changed := pool.resolveTokenForCookie(store.Cookie{
		AccountID: "account-1",
		Value:     "cookie=better-auth.session_token=shared\ntoken=" + token,
	})
	if changed || got != token {
		t.Fatalf("resolveTokenForCookie = (%q, %v), want matching fallback", got, changed)
	}
}
