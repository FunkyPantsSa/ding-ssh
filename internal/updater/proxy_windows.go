package updater

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"net/url"
	"strings"
)

func systemProxy() (*url.URL, error) {
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if e != nil {
		return nil, e
	}
	defer k.Close()
	enabled, _, _ := k.GetIntegerValue("ProxyEnable")
	if enabled == 0 {
		pac, _, _ := k.GetStringValue("AutoConfigURL")
		if pac != "" {
			return nil, fmt.Errorf("系统使用 PAC，请配置 HTTPS_PROXY 或手动系统代理")
		}
		return nil, nil
	}
	server, _, e := k.GetStringValue("ProxyServer")
	if e != nil {
		return nil, e
	}
	if strings.Contains(server, "=") {
		parts := strings.Split(server, ";")
		server = ""
		for _, proto := range []string{"https=", "http=", "socks="} {
			for _, p := range parts {
				if strings.HasPrefix(p, proto) {
					server = strings.TrimPrefix(p, proto)
					if proto == "socks=" {
						server = "socks5://" + server
					}
					break
				}
			}
			if server != "" {
				break
			}
		}
	}
	if server == "" {
		return nil, fmt.Errorf("系统代理地址为空")
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return url.Parse(server)
}
