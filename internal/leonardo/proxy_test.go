package leonardo

import (
	"net/http"
	"testing"
)

func TestNormalizeProxyURL(t *testing.T) {
	tests := map[string]string{
		"":                      "",
		"127.0.0.1:7897":        "http://127.0.0.1:7897",
		"http://127.0.0.1:7897": "http://127.0.0.1:7897",
		"http=127.0.0.1:7890;https=127.0.0.1:7897": "http://127.0.0.1:7897",
	}
	for input, want := range tests {
		if got := normalizeProxyURL(input); got != want {
			t.Fatalf("normalizeProxyURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDynamicProxyForRequestReloadsProxyAndBypassesLoopback(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	t.Setenv("https_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")

	external, _ := http.NewRequest(http.MethodGet, "https://app.leonardo.ai/api/auth/get-session", nil)
	proxy, err := dynamicProxyForRequest(external)
	if err != nil {
		t.Fatalf("dynamicProxyForRequest external error: %v", err)
	}
	if proxy == nil || proxy.String() != "http://127.0.0.1:7897" {
		t.Fatalf("dynamicProxyForRequest external = %v, want configured proxy", proxy)
	}

	loopback, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8001/v1/models", nil)
	proxy, err = dynamicProxyForRequest(loopback)
	if err != nil {
		t.Fatalf("dynamicProxyForRequest loopback error: %v", err)
	}
	if proxy != nil {
		t.Fatalf("dynamicProxyForRequest loopback = %s, want direct", proxy.String())
	}
}

func TestDynamicProxyForRequestObservesProxyEnabledAfterClientStartup(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")

	previous := systemProxyLookup
	t.Cleanup(func() { systemProxyLookup = previous })
	current := ""
	systemProxyLookup = func() string { return current }
	req, _ := http.NewRequest(http.MethodGet, "https://app.leonardo.ai/api/auth/get-session", nil)

	proxy, err := dynamicProxyForRequest(req)
	if err != nil || proxy != nil {
		t.Fatalf("proxy before enable = %v, err=%v; want direct", proxy, err)
	}
	current = "127.0.0.1:7897"
	proxy, err = dynamicProxyForRequest(req)
	if err != nil || proxy == nil || proxy.String() != "http://127.0.0.1:7897" {
		t.Fatalf("proxy after enable = %v, err=%v; want live system proxy", proxy, err)
	}
}
