//go:build !windows

package apphost

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"ding-ssh/internal/logx"
)

// revealLocalPath 在系统文件管理器中打开 path：
//   - macOS：open 打开目录，open -R 在访达中选中文件；
//   - 其他（Linux 等）：xdg-open 打开目录，文件则打开其所在目录。
//
// 命令同步执行：open / xdg-open 会立即返回（文件管理器在后台接管），因此这里等待退出
// 可以拿到真实的启动错误并回传给用户。
func revealLocalPath(path string) error {
	logx.Debugf("[apphost] 准备在系统文件管理器中打开路径: path=%q", path)
	abs, err := filepath.Abs(path)
	if err != nil {
		logx.Warnf("[apphost] 解析本地路径绝对路径失败: path=%q err=%v", path, err)
		return err
	}
	info, statErr := os.Stat(abs)
	isDir := statErr == nil && info.IsDir()
	if statErr != nil {
		// 打开不存在的文件时按「文件」处理（-R / 打开父目录），由文件管理器给出最终提示。
		logx.Debugf("[apphost] 路径属性读取失败，按文件处理: path=%q err=%v", abs, statErr)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if isDir {
			cmd = exec.Command("open", abs)
		} else {
			cmd = exec.Command("open", "-R", abs)
		}
	default:
		target := abs
		if !isDir {
			target = filepath.Dir(abs)
		}
		cmd = exec.Command("xdg-open", target)
	}
	logx.Debugf("[apphost] 执行文件管理器命令: cmd=%q isDir=%v", cmd.Args, isDir)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		// 这里是辅助函数：错误由上层 App.RevealLocalPath 统一记 ERROR，避免重复记录。
		logx.Warnf("[apphost] 文件管理器命令执行失败: cmd=%q err=%v output=%q", cmd.Args, runErr, string(out))
		return fmt.Errorf("%w（%s）", runErr, string(out))
	}
	return nil
}
