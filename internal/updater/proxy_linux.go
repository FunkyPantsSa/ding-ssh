//go:build linux

package updater

import (
	"ding-ssh/internal/i18n"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"ding-ssh/internal/logx"
)

func systemProxy() (*url.URL, error) {
	get := func(args ...string) string {
		b, err := exec.Command("gsettings", args...).Output()
		if err != nil {
			logx.Debugf("[updater] 读取 gsettings 失败（可能未安装 GNOME）: args=%v err=%v", args, err)
			return ""
		}
		return strings.Trim(strings.TrimSpace(string(b)), "'\"")
	}
	mode := get("get", "org.gnome.system.proxy", "mode")
	logx.Debugf("[updater] 检测 GNOME 代理模式: mode=%q", mode)
	if mode == "auto" {
		logx.Warnf("[updater] 系统使用 PAC 代理，无法自动解析，需设置 HTTPS_PROXY 或手动代理")
		return nil, fmt.Errorf("%s", i18n.T("系统使用 PAC，请设置 HTTPS_PROXY 或使用手动代理"))
	}
	if mode != "manual" {
		return nil, nil
	}
	for _, kind := range []string{"https", "http", "socks"} {
		schema := "org.gnome.system.proxy." + kind
		host := get("get", schema, "host")
		port := get("get", schema, "port")
		if host != "" && port != "0" {
			scheme := "http"
			if kind == "socks" {
				scheme = "socks5"
			}
			logx.Debugf("[updater] 已检测到系统代理: kind=%s host=%s port=%s", kind, host, port)
			return url.Parse(scheme + "://" + host + ":" + port)
		}
	}
	logx.Debugf("[updater] 系统设置为手动代理但未配置有效地址")
	return nil, nil
}
