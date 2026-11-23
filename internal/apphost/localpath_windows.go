//go:build windows

package apphost

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// revealLocalPath 在资源管理器中打开 path：
//   - path 是目录 → 直接打开该目录；
//   - path 是文件 → 打开其所在目录并选中该文件（explorer /select,）。
//
// 为什么不用 `cmd /c start`：start 会把 & 等字符当作命令分隔符，含空格或 & 的路径会被截断
// 或直接报错。explorer.exe 走参数数组，路径原样传递，对特殊字符安全。
//
// 注意：explorer.exe 即使成功也可能返回非 0 退出码（Windows 既有行为），因此这里只把
// 「进程启动失败」当作错误，退出码交由后台回收时忽略。
func revealLocalPath(path string) error {
	logx.Debugf("[apphost] 准备在资源管理器中打开路径: path=%q", path)
	abs, err := filepath.Abs(path)
	if err != nil {
		logx.Warnf("[apphost] 解析本地路径绝对路径失败: path=%q err=%v", path, err)
		return err
	}
	args := []string{abs}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		// /select, 与路径之间不能有空格：explorer 把「/select,」后紧跟的内容当作路径。
		args = []string{"/select," + abs}
	} else if statErr != nil {
		// 路径不存在时按「打开目录」处理，由资源管理器给出最终提示。
		logx.Debugf("[apphost] 路径属性读取失败，按目录处理: path=%q err=%v", abs, statErr)
	}
	logx.Debugf("[apphost] 执行 explorer 命令: args=%q", args)
	cmd := exec.Command("explorer.exe", args...)
	if err := cmd.Start(); err != nil {
		// 辅助函数只记 WARN，用户可见的 ERROR 由上层 App.RevealLocalPath 统一记录。
		logx.Warnf("[apphost] 启动资源管理器失败: args=%q err=%v", args, err)
		return fmt.Errorf(i18n.T("启动资源管理器失败: %w"), err)
	}
	// 不等待进程退出（资源管理器会复用已有窗口并长期驻留），后台回收句柄即可。
	go func() { _ = cmd.Wait() }()
	return nil
}
