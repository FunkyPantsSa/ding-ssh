//go:build !windows

package updater

import (
	"ding-ssh/internal/i18n"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func elevatedHelper() (bool, error) { return false, nil }

func installWithPermissions(p Plan, _ string, _ []byte) error { return applyPlan(p) }

func showInstallError(error) {}

func waitForParent(pid int) error {
	for i := 0; i < 300; i++ {
		if exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("%s", i18n.T("等待主进程退出超时，更新未安装"))
}
