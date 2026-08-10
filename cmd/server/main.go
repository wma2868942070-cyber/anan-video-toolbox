package main

import (
	"bufio"
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/config"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/server"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

func main() {
	loadLocalEnvFile(".env.server.local")
	cfg := config.Load()
	if cfg.APIKey == "" && !isLoopbackHost(cfg.Host) {
		log.Fatal("ANAN_VIDEO_TOOLBOX_API_KEY is required when listening on a non-loopback address")
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if err := st.Bootstrap(cfg.ModelFile); err != nil {
		log.Fatalf("bootstrap store: %v", err)
	}
	if merged, err := st.MergeDuplicateCookieAccounts(); err != nil {
		log.Printf("merge duplicate Leonardo accounts: %v", err)
	} else if merged > 0 {
		log.Printf("merged %d duplicate Leonardo account rows", merged)
	}

	client := leonardo.New()
	svc := service.NewLeonardoPool(st, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	browserWorkspaces := leonardo.NewBrowserWorkspaceManager(filepath.Dir(cfg.DBPath))
	go startLeonardoAutoRefresh(ctx, svc, browserWorkspaces)

	srv := server.New(cfg, st, svc)
	addr := cfg.Addr()
	log.Printf("anan视频工具箱 API listening on %s", addr)
	if err := srv.Run(addr); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func startLeonardoAutoRefresh(ctx context.Context, svc *service.LeonardoPool, browserWorkspaces *leonardo.BrowserWorkspaceManager) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	sweeps := 0
	var lastBrowserRecovery time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			needsBrowserRecovery := false
			if res, err := svc.RefreshExpiringCookieSessions(); err != nil {
				log.Printf("Leonardo proactive refresh failed: %v", err)
			} else if res.Checked > 0 {
				log.Printf("Leonardo proactive refresh: %d/%d successful", res.OK, res.Checked)
				needsBrowserRecovery = res.OK < res.Checked
			}
			sweeps++
			if sweeps == 1 || sweeps%10 == 0 {
				if res, err := svc.RecoverExpiredCookies(); err != nil {
					log.Printf("Leonardo legacy session recovery failed: %v", err)
				} else if res.Checked > 0 {
					log.Printf("Leonardo legacy session recovery: %d/%d successful, %d re-enabled", res.OK, res.Checked, res.Reenabled)
					needsBrowserRecovery = needsBrowserRecovery || res.OK < res.Checked
				}
			}
			// Only fall back to the persistent Chrome profiles when the lightweight
			// Cookie -> JWT renewal failed. This avoids keeping browsers running or
			// adding normal startup overhead, while still recovering a rotated
			// HttpOnly Cookie without requiring the user to copy cURL every hour.
			if needsBrowserRecovery && (lastBrowserRecovery.IsZero() || time.Since(lastBrowserRecovery) >= 10*time.Minute) {
				lastBrowserRecovery = time.Now()
				checked, recovered := recoverLeonardoBrowserWorkspaces(ctx, svc, browserWorkspaces)
				if checked > 0 {
					log.Printf("Leonardo browser workspace recovery: %d/%d successful", recovered, checked)
				}
			}
			timer.Reset(time.Minute)
		}
	}
}

func recoverLeonardoBrowserWorkspaces(ctx context.Context, svc *service.LeonardoPool, manager *leonardo.BrowserWorkspaceManager) (checked, recovered int) {
	if manager == nil {
		return 0, 0
	}
	workspaces, err := manager.List()
	if err != nil {
		return 0, 0
	}
	for _, workspace := range workspaces {
		if ctx.Err() != nil {
			break
		}
		expectedAccountID := strings.TrimSpace(workspace.AccountID)
		if expectedAccountID == "" {
			// Legacy/unimported profiles have no trustworthy owner. The user must
			// import each once so recovery cannot silently refresh the wrong row.
			continue
		}
		checked++
		readCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		cookie, readErr := manager.ReadCookie(readCtx, workspace.ID)
		cancel()
		if readErr != nil || strings.TrimSpace(cookie) == "" {
			continue
		}
		if _, err := svc.RefreshBoundCookieValidated(expectedAccountID, "cookie="+cookie); err == nil {
			recovered++
		}
	}
	return checked, recovered
}

// loadLocalEnvFile makes the packaged Windows server start consistently when
// launched by double-click or Start-Process. Existing environment variables
// always win, and values are never printed.
func loadLocalEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || os.Getenv(key) != "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		_ = os.Setenv(key, value)
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
