//go:build linux

package updater

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

func systemProxy() (*url.URL, error) {
	get := func(args ...string) string {
		b, _ := exec.Command("gsettings", args...).Output()
		return strings.Trim(strings.TrimSpace(string(b)), "'\"")
	}
	mode := get("get", "org.gnome.system.proxy", "mode")
	if mode == "auto" {
		return nil, fmt.Errorf("系统使用 PAC，请设置 HTTPS_PROXY 或使用手动代理")
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
			return url.Parse(scheme + "://" + host + ":" + port)
		}
	}
	return nil, nil
}
