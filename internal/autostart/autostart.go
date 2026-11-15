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
)

func Set(enabled bool) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var path, content string
	switch runtime.GOOS {
	case "windows":
		args := []string{"delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh", "/f"}
		if enabled {
			args = []string{"add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh", "/t", "REG_SZ", "/d", `"` + executable + `"`, "/f"}
		} else {
			if exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ding-ssh").Run() != nil {
				return nil
			}
		}
		output, err := exec.Command("reg", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("autostart: %w: %s", err, output)
		}
		return nil
	case "darwin":
		path = filepath.Join(home, "Library", "LaunchAgents", "app.ding-ssh.login.plist")
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(executable)); err != nil {
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
		}
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", "\\$", "%", "%%").Replace(executable)
		content = "[Desktop Entry]\nType=Application\nName=ding-ssh\nExec=\"" + escaped + "\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
	if !enabled {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}
