//go:build !windows

package desktop

import "errors"

func restartLeonardoGateway() error {
	return errors.New("重启 Leonardo 本地服务目前仅支持 Windows")
}
