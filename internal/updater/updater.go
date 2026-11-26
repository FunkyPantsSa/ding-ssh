package updater

import (
	"context"
	"crypto/sha256"
	"ding-ssh/internal/i18n"
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

	"ding-ssh/internal/logx"
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
	logx.Debugf("[updater] 创建更新 HTTP 客户端: timeout=30m envProxy=%v",
		os.Getenv("HTTPS_PROXY") != "" || os.Getenv("https_proxy") != "" ||
			os.Getenv("HTTP_PROXY") != "" || os.Getenv("http_proxy") != "")
	return &http.Client{Transport: transport, Timeout: 30 * time.Minute}
}
func get(ctx context.Context, c *http.Client, u string) (*http.Response, error) {
	logx.Debugf("[updater] 发起更新请求: url=%s", u)
	r, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		logx.Warnf("[updater] 构造更新请求失败: url=%s err=%v", u, err)
		return nil, err
	}
	res, err := c.Do(r)
	if err != nil {
		logx.Warnf("[updater] 更新请求失败: url=%s err=%v", u, err)
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		logx.Warnf("[updater] 更新请求返回非 200: url=%s status=%s", u, res.Status)
		return nil, fmt.Errorf(i18n.T("更新服务器返回 %s"), res.Status)
	}
	logx.Debugf("[updater] 更新请求成功: url=%s status=%d contentLength=%d", u, res.StatusCode, res.ContentLength)
	return res, nil
}
func Check(ctx context.Context, channel string) (Manifest, error) {
	start := time.Now()
	var m Manifest
	logx.Infof("[updater] 检查更新: channel=%s currentCommit=%s", channel, BuildCommit)
	if channel != "dev" && channel != "main" {
		logx.Warnf("[updater] 检查更新被拒绝: 无效通道 channel=%q", channel)
		return m, fmt.Errorf("%s", i18n.T("无效更新通道"))
	}
	res, err := get(ctx, Client(), repository+"/releases/download/"+channel+"-latest/update.json")
	if err != nil {
		logx.Errorf("[updater] 检查更新失败: channel=%s err=%v", channel, err)
		return m, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&m)
	if err != nil {
		logx.Errorf("[updater] 解析更新清单失败: channel=%s err=%v", channel, err)
		return m, err
	}
	if m.Channel != channel || len(m.Commit) != 40 {
		logx.Errorf("[updater] 更新清单无效: channel=%s manifestChannel=%q commitLen=%d", channel, m.Channel, len(m.Commit))
		return m, fmt.Errorf("%s", i18n.T("更新清单无效"))
	}
	if m.Commit == BuildCommit {
		logx.Infof("[updater] 当前已是最新版本: channel=%s commit=%s elapsed=%s", channel, m.Commit, time.Since(start))
	} else {
		logx.Infof("[updater] 发现新版本: channel=%s commit=%s builtAt=%s assets=%d elapsed=%s",
			channel, m.Commit, m.BuiltAt, len(m.Assets), time.Since(start))
	}
	return m, nil
}
func Download(ctx context.Context, m Manifest, progress func(int64, int64)) (string, error) {
	start := time.Now()
	key := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "" {
		key += "-appimage"
	}
	a, ok := m.Assets[key]
	if !ok {
		logx.Errorf("[updater] 下载更新失败: 当前平台暂无更新包 platform=%s assets=%d", key, len(m.Assets))
		return "", fmt.Errorf("%s", i18n.T("当前平台暂无更新包"))
	}
	logx.Infof("[updater] 开始下载更新包: channel=%s asset=%s platform=%s", m.Channel, a.Name, key)
	if filepath.Base(a.Name) != a.Name || strings.ContainsAny(a.Name, "/\\") || len(a.SHA256) != 64 {
		logx.Errorf("[updater] 下载更新失败: 更新包信息无效 asset=%s sha256Len=%d", a.Name, len(a.SHA256))
		return "", fmt.Errorf("%s", i18n.T("更新包信息无效"))
	}
	dir, err := os.MkdirTemp("", "ding-ssh-update-")
	if err != nil {
		logx.Errorf("[updater] 创建更新临时目录失败: err=%v", err)
		return "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	logx.Debugf("[updater] 更新下载临时目录: dir=%q", dir)
	res, err := get(ctx, Client(), repository+"/releases/download/"+m.Channel+"-latest/"+url.PathEscape(a.Name))
	if err != nil {
		logx.Errorf("[updater] 下载更新包失败: asset=%s err=%v", a.Name, err)
		return "", err
	}
	defer res.Body.Close()
	file := filepath.Join(dir, a.Name)
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		logx.Errorf("[updater] 创建更新包文件失败: path=%q err=%v", file, err)
		return "", err
	}
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	var total int64
	var milestone int64
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			if _, err = io.MultiWriter(f, hash).Write(buf[:n]); err != nil {
				f.Close()
				logx.Errorf("[updater] 写更新包失败: path=%q written=%d err=%v", file, total, err)
				return "", err
			}
			total += int64(n)
			progress(total, res.ContentLength)
			// 进度日志只记 25/50/75/100 里程碑，避免每块一条。
			if res.ContentLength > 0 {
				pct := total * 100 / res.ContentLength
				if next := (milestone + 1) * 25; pct >= next && next <= 100 {
					milestone++
					logx.Debugf("[updater] 更新包下载进度: asset=%s percent=%d bytes=%d/%d",
						a.Name, milestone*25, total, res.ContentLength)
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			f.Close()
			logx.Errorf("[updater] 读取更新包数据失败: asset=%s received=%d err=%v", a.Name, total, e)
			return "", e
		}
		if total > 2<<30 {
			f.Close()
			logx.Errorf("[updater] 更新包过大，中止下载: asset=%s received=%d", a.Name, total)
			return "", fmt.Errorf("%s", i18n.T("更新包过大"))
		}
	}
	if err = f.Close(); err != nil {
		logx.Errorf("[updater] 关闭更新包文件失败: path=%q err=%v", file, err)
		return "", err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), a.SHA256) {
		logx.Errorf("[updater] 更新包校验失败: asset=%s bytes=%d", a.Name, total)
		return "", fmt.Errorf("%s", i18n.T("更新包校验失败"))
	}
	success = true
	logx.Infof("[updater] 更新包下载完成: asset=%s bytes=%d path=%q elapsed=%s",
		a.Name, total, file, time.Since(start))
	return file, nil
}
