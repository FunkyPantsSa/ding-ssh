//go:build darwin

package updater

import (
	"fmt"
	"github.com/oomol-lab/sysproxy"
	"net/url"
)

func systemProxy() (*url.URL, error) {
	p, err := sysproxy.GetHTTPS()
	if err != nil {
		return nil, err
	}
	if p == nil {
		p, err = sysproxy.GetHTTP()
	}
	if err != nil || p == nil {
		return nil, err
	}
	return url.Parse(fmt.Sprintf("http://%s:%d", p.Host, p.Port))
}
