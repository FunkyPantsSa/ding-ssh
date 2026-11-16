package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/net/http/httpproxy"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var BuildChannel = "main"

// BuildCommit is injected by CI because Wails builds may omit Go VCS metadata.
var BuildCommit string

const repository = "https://github.com/FunkyPantsSa/ding-ssh"

type Manifest struct {
	Commit  string           `json:"commit"`
	Channel string           `json:"channel"`
	BuiltAt string           `json:"builtAt"`
	Assets  map[string]Asset `json:"assets"`
}
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func Client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		p, err := http.ProxyFromEnvironment(r)
		if err != nil || p != nil {
			return p, err
		}
		if os.Getenv("HTTPS_PROXY") != "" || os.Getenv("https_proxy") != "" || os.Getenv("HTTP_PROXY") != "" || os.Getenv("http_proxy") != "" {
			return nil, nil
		}
		p, err = systemProxy()
		if err != nil || p == nil {
			return p, err
		}
		noProxy := os.Getenv("NO_PROXY")
		if noProxy == "" {
			noProxy = os.Getenv("no_proxy")
		}
		config := httpproxy.Config{HTTPProxy: p.String(), HTTPSProxy: p.String(), NoProxy: noProxy}
		return config.ProxyFunc()(r.URL)
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Minute}
}
func get(ctx context.Context, c *http.Client, u string) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(r)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("更新服务器返回 %s", res.Status)
	}
	return res, nil
}
func Check(ctx context.Context, channel string) (Manifest, error) {
	var m Manifest
	if channel != "dev" && channel != "main" {
		return m, fmt.Errorf("无效更新通道")
	}
	res, err := get(ctx, Client(), repository+"/releases/download/"+channel+"-latest/update.json")
	if err != nil {
		return m, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&m)
	if err != nil {
		return m, err
	}
	if m.Channel != channel || len(m.Commit) != 40 {
		return m, fmt.Errorf("更新清单无效")
	}
	return m, nil
}
func Download(ctx context.Context, m Manifest, progress func(int64, int64)) (string, error) {
	key := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "" {
		key += "-appimage"
	}
	a, ok := m.Assets[key]
	if !ok {
		return "", fmt.Errorf("当前平台暂无更新包")
	}
	if filepath.Base(a.Name) != a.Name || strings.ContainsAny(a.Name, "/\\") || len(a.SHA256) != 64 {
		return "", fmt.Errorf("更新包信息无效")
	}
	dir, err := os.MkdirTemp("", "ding-ssh-update-")
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	res, err := get(ctx, Client(), repository+"/releases/download/"+m.Channel+"-latest/"+url.PathEscape(a.Name))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	file := filepath.Join(dir, a.Name)
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	var total int64
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			if _, err = io.MultiWriter(f, hash).Write(buf[:n]); err != nil {
				f.Close()
				return "", err
			}
			total += int64(n)
			progress(total, res.ContentLength)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			f.Close()
			return "", e
		}
		if total > 2<<30 {
			f.Close()
			return "", fmt.Errorf("更新包过大")
		}
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), a.SHA256) {
		return "", fmt.Errorf("更新包校验失败")
	}
	success = true
	return file, nil
}
