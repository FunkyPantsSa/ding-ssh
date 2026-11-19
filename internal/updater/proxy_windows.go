package updater

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"net/url"
	"strings"

	"ding-ssh/internal/logx"
)

func systemProxy() (*url.URL, error) {
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if e != nil {
		logx.Debugf("[updater] 打开 Windows Internet Settings 注册表失败: err=%v", e)
		return nil, e
	}
	defer k.Close()
	enabled, _, _ := k.GetIntegerValue("ProxyEnable")
	if enabled == 0 {
		pac, _, _ := k.GetStringValue("AutoConfigURL")
		if pac != "" {
			logx.Warnf("[updater] 系统使用 PAC 代理，无法自动解析，需配置 HTTPS_PROXY 或手动系统代理")
			return nil, fmt.Errorf("系统使用 PAC，请配置 HTTPS_PROXY 或手动系统代理")
		}
		logx.Debugf("[updater] 系统代理未启用")
		return nil, nil
	}
	server, _, e := k.GetStringValue("ProxyServer")
	if e != nil {
		logx.Debugf("[updater] 读取系统代理地址失败: err=%v", e)
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
		logx.Warnf("[updater] 系统代理已启用但地址为空")
		return nil, fmt.Errorf("系统代理地址为空")
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	u, err := url.Parse(server)
	if err != nil {
		logx.Debugf("[updater] 解析系统代理地址失败: err=%v", err)
		return nil, err
	}
	// 只记协议与主机名：ProxyServer 可能内嵌用户名/密码，不能整串落日志。
	logx.Debugf("[updater] 已检测到系统代理: scheme=%s host=%s", u.Scheme, u.Hostname())
	return u, nil
}
