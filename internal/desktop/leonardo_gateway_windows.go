//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func restartLeonardoGateway() error {
	serverPath, err := findLeonardoGatewayExecutable()
	if err != nil {
		return err
	}
	workingDir := filepath.Dir(filepath.Dir(serverPath))
	quotedServer := strings.ReplaceAll(serverPath, "'", "''")
	quotedWorkDir := strings.ReplaceAll(workingDir, "'", "''")
	script := fmt.Sprintf(`
$server = '%s'
$workdir = '%s'
$listeners = Get-NetTCPConnection -LocalPort 8001 -State Listen -ErrorAction SilentlyContinue
foreach ($listener in $listeners) {
  $owner = Get-CimInstance Win32_Process -Filter ("ProcessId = " + $listener.OwningProcess) -ErrorAction SilentlyContinue
  if ($owner -and [string]::Equals([System.IO.Path]::GetFullPath($owner.ExecutablePath), [System.IO.Path]::GetFullPath($server), [System.StringComparison]::OrdinalIgnoreCase)) {
    Stop-Process -Id $listener.OwningProcess -Force -ErrorAction Stop
  }
}
Start-Sleep -Milliseconds 400
Start-Process -FilePath $server -WorkingDirectory $workdir -WindowStyle Hidden
`, quotedServer, quotedWorkDir)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("重启 8001 本地服务失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func findLeonardoGatewayExecutable() (string, error) {
	var candidates []string
	if configured := strings.TrimSpace(os.Getenv("ANAN_VIDEO_TOOLBOX_SERVER_EXE")); configured != "" {
		candidates = append(candidates, configured)
	}
	if executable, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "..", "..", "dist", "anan-video-toolbox-server.exe"),
			filepath.Join(exeDir, "anan-video-toolbox-server.exe"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "dist", "anan-video-toolbox-server.exe"),
			filepath.Join(cwd, "..", "..", "..", "dist", "anan-video-toolbox-server.exe"),
		)
	}
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil || seen[strings.ToLower(abs)] {
			continue
		}
		seen[strings.ToLower(abs)] = true
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, nil
		}
	}
	return "", errors.New("未找到 dist\anan-video-toolbox-server.exe，请先重新构建本地服务")
}
