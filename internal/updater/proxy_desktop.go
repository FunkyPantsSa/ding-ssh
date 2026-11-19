//go:build darwin

package updater

import (
	"fmt"
	"github.com/oomol-lab/sysproxy"
	"net/url"

	"ding-ssh/internal/logx"
)

func systemProxy() (*url.URL, error) {
	p, err := sysproxy.GetHTTPS()
	if err != nil {
		logx.Debugf("[updater] 读取系统 HTTPS 代理失败，尝试 HTTP 代理: err=%v", err)
		return nil, err
	}
	if p == nil {
		p, err = sysproxy.GetHTTP()
	}
	if err != nil || p == nil {
		logx.Debugf("[updater] 未检测到可用的系统代理: err=%v", err)
		return nil, err
	}
	logx.Debugf("[updater] 已检测到系统代理: host=%s port=%d", p.Host, p.Port)
	return url.Parse(fmt.Sprintf("http://%s:%d", p.Host, p.Port))
}
