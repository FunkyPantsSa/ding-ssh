// Package autostart manages only ding-ssh's per-user login entry.
package autostart

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"ding-ssh/internal/logx"
)

func Set(enabled bool) error {
	executable, err := os.Executable()
	if err != nil {
		logx.Errorf("[autostart] 获取当前可执行文件失败: err=%v", err)
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		logx.Errorf("[autostart] 解析可执行文件绝对路径失败: path=%q err=%v", executable, err)
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		logx.Errorf("[autostart] 获取用户主目录失败: err=%v", err)
		return err
	}
	logx.Infof("[autostart] 设置开机自启: enabled=%v platform=%s executable=%q", enabled, runtime.GOOS, executable)
	var path, content string
	switch runtime.GOOS {
	case "windows":
		args := []string{"delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh", "/f"}
		if enabled {
			args = []string{"add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh", "/t", "REG_SZ", "/d", `"` + executable + `"`, "/f"}
			logx.Debugf("[autostart] 写入 Windows 注册表自启项: key=HKCU\\...\\Run value=ding-ssh")
		} else {
			if exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh").Run() != nil {
				logx.Warnf("[autostart] Windows 自启项不存在（可能已被手动移除），无需删除: value=ding-ssh")
				return nil
			}
			logx.Debugf("[autostart] 删除 Windows 注册表自启项: value=ding-ssh")
		}
		output, err := exec.Command("reg", args...).CombinedOutput()
		if err != nil {
			logx.Errorf("[autostart] 操作 Windows 注册表自启项失败: enabled=%v err=%v output=%q", enabled, err, string(output))
			return fmt.Errorf("autostart: %w: %s", err, output)
		}
		logx.Infof("[autostart] Windows 注册表自启项已更新: enabled=%v", enabled)
		return nil
	case "darwin":
		path = filepath.Join(home, "Library", "LaunchAgents", "app.ding-ssh.login.plist")
		logx.Debugf("[autostart] 使用 macOS LaunchAgent 实现开机自启: path=%q", path)
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(executable)); err != nil {
			logx.Errorf("[autostart] 转义 plist 可执行路径失败: path=%q err=%v", executable, err)
			return err
		}
		content = `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>app.ding-ssh.login</string><key>ProgramArguments</key><array><string>` + escaped.String() + `</string></array><key>RunAtLoad</key><true/></dict></plist>`
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(base) {
			base = filepath.Join(home, ".config")
		}
		path = filepath.Join(base, "autostart", "ding-ssh.desktop")
		// AppImage's internal executable is mounted at a transient path.
		if image := os.Getenv("APPIMAGE"); filepath.IsAbs(image) {
			executable = image
			logx.Debugf("[autostart] 检测到 AppImage，自启目标改为镜像路径: path=%q", image)
		}
		logx.Debugf("[autostart] 使用 Linux XDG autostart 实现开机自启: path=%q", path)
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", "\\$", "%", "%%").Replace(executable)
		content = "[Desktop Entry]\nType=Application\nName=ding-ssh\nExec=\"" + escaped + "\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	default:
		logx.Errorf("[autostart] 当前平台不支持开机自启: platform=%s", runtime.GOOS)
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
	if !enabled {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			logx.Warnf("[autostart] 自启文件不存在（可能已被手动移除），无需移除: path=%q", path)
			return nil
		}
		if err != nil {
			logx.Errorf("[autostart] 移除开机自启文件失败: path=%q err=%v", path, err)
			return err
		}
		logx.Infof("[autostart] 开机自启已关闭: path=%q", path)
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		logx.Errorf("[autostart] 创建自启文件目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return err
	}
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		logx.Errorf("[autostart] 写入自启文件失败: path=%q bytes=%d err=%v", path, len(content), err)
		return err
	}
	logx.Infof("[autostart] 开机自启已开启: path=%q", path)
	return nil
}
