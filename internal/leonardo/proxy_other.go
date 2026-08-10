//go:build !windows

package leonardo

func systemProxyURL() string { return "" }
