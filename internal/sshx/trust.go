package sshx

import (
	"bufio"
	"ding-ssh/internal/i18n"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var trustMu sync.Mutex

type HostTrustRecord struct {
	Line        int    `json:"line"`
	Hosts       string `json:"hosts"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	Source      string `json:"source"`
	ReadOnly    bool   `json:"readOnly"`
}

func trustPaths(extra []string) []string {
	paths := []string{filepath.Join(appdir.ConfigDir(), "known_hosts")}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".ssh", "known_hosts"))
	}
	return append(paths, extra...)
}

// VerifyHostKey never writes trust. Unknown keys and changed/revoked keys are errors.
func VerifyHostKey(host string, remote net.Addr, key ssh.PublicKey, extra []string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	fingerprint := ssh.FingerprintSHA256(key)
	logx.Debugf("[sshx] 校验主机密钥: host=%s keyType=%s fingerprint=%s extraSources=%d", host, key.Type(), fingerprint, len(extra))
	for _, source := range extra {
		if _, err := os.Stat(expandSSHPath(source)); err != nil {
			return fmt.Errorf(i18n.T("读取指定信任源失败: %w"), err)
		}
	}
	paths := trustPaths(nil)
	for _, source := range extra {
		paths = append(paths, expandSSHPath(source))
	}
	verify, err := preferredHostKeyVerifier(paths)
	if err != nil {
		return err
	}
	if err := verify(host, remote, key); err != nil {
		logx.Warnf("[sshx] 主机密钥未通过校验: host=%s keyType=%s fingerprint=%s err=%v", host, key.Type(), fingerprint, err)
		return err
	}
	logx.Debugf("[sshx] 主机密钥校验通过: host=%s fingerprint=%s", host, fingerprint)
	return nil
}

func ListHostTrust() ([]HostTrustRecord, error) {
	trustMu.Lock()
	defer trustMu.Unlock()
	result := []HostTrustRecord{}
	start := time.Now()
	for i, file := range trustPaths(nil) {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		before := len(result)
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		line := 0
		for scanner.Scan() {
			line++
			text := strings.TrimSpace(scanner.Text())
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			fields := strings.Fields(text)
			if strings.HasPrefix(fields[0], "@") {
				fields = fields[1:]
			}
			if len(fields) < 3 {
				logx.Debugf("[sshx] 无效主机信任记录: path=%q line=%d", file, line)
				return nil, fmt.Errorf(i18n.T("无效信任记录 %s:%d"), file, line)
			}
			key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
			if err != nil {
				return nil, err
			}
			result = append(result, HostTrustRecord{line, fields[0], key.Type(), ssh.FingerprintSHA256(key), file, i != 0})
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		logx.Debugf("[sshx] 主机信任库已读取: path=%q records=%d", file, len(result)-before)
	}
	logx.Infof("[sshx] 主机信任记录已加载: records=%d sources=%d elapsed=%s", len(result), len(trustPaths(nil)), time.Since(start))
	return result, nil
}

// SaveHostTrust only adds an explicitly approved public key, refusing existing
// conflicts and revocations across all default sources.
func SaveHostTrust(host, authorizedKey string) error {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		logx.Debugf("[sshx] 解析待保存主机公钥失败: host=%s err=%v", host, err)
		return err
	}
	fingerprint := ssh.FingerprintSHA256(key)
	trustMu.Lock()
	defer trustMu.Unlock()
	verify, err := loadHostKeyVerifier(trustPaths(nil))
	if err != nil {
		return err
	}
	err = verify(host, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key)
	if err == nil {
		logx.Debugf("[sshx] 主机已受信任，无需新增记录: host=%s fingerprint=%s", host, fingerprint)
		return nil
	}
	var unknown *knownhosts.KeyError
	if !errors.As(err, &unknown) || len(unknown.Want) > 0 {
		logx.Warnf("[sshx] 拒绝覆盖已有主机密钥: host=%s fingerprint=%s err=%v", host, fingerprint, err)
		return err
	}
	file := trustPaths(nil)[0]
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte(knownhosts.Line([]string{host}, key)+"\n")...)
	if err := writeTrustFile(file, data); err != nil {
		logx.Debugf("[sshx] 写入主机信任库失败: host=%s path=%q err=%v", host, file, err)
		return err
	}
	logx.Infof("[sshx] 新增主机信任记录: host=%s keyType=%s fingerprint=%s", host, key.Type(), fingerprint)
	return nil
}

func DeleteHostTrust(line int, fingerprint string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	file := trustPaths(nil)[0]
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		logx.Warnf("[sshx] 删除主机信任记录失败（行号越界）: line=%d totalLines=%d", line, len(lines))
		return fmt.Errorf("%s", i18n.T("记录已变化，请刷新"))
	}
	fields := strings.Fields(lines[line-1])
	if len(fields) < 3 {
		return fmt.Errorf("%s", i18n.T("无效记录"))
	}
	if strings.HasPrefix(fields[0], "@") {
		logx.Warnf("[sshx] 拒绝删除撤销记录: line=%d", line)
		return fmt.Errorf("%s", i18n.T("撤销记录不能从此界面删除"))
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
	if err != nil || ssh.FingerprintSHA256(key) != fingerprint {
		logx.Warnf("[sshx] 删除主机信任记录失败（记录已变化）: line=%d err=%v", line, err)
		return fmt.Errorf("%s", i18n.T("记录已变化，请刷新"))
	}
	lines = append(lines[:line-1], lines[line:]...)
	if err := writeTrustFile(file, []byte(strings.Join(lines, "\n"))); err != nil {
		return err
	}
	logx.Infof("[sshx] 删除主机信任记录: line=%d fingerprint=%s remaining=%d", line, fingerprint, len(lines))
	return nil
}

func writeTrustFile(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".known-hosts-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), file)
}

func verifyConnectionHost(host string, remote net.Addr, key ssh.PublicKey) error {
	return verifyConnectionHostSources(host, remote, key, nil)
}

func verifyConnectionHostSources(host string, remote net.Addr, key ssh.PublicKey, sources []string) error {
	err := VerifyHostKey(host, remote, key, sources)
	if err == nil {
		return nil
	}
	return hostTrustChallenge(host, key, sources, err)
}

// Application pins take precedence over stale read-only keys. Every source is
// still parsed and its revocation list checked before applying the pin.
func preferredHostKeyVerifier(paths []string) (ssh.HostKeyCallback, error) {
	all, err := loadHostKeyVerifier(paths)
	if err != nil {
		return nil, err
	}
	app, err := loadHostKeyVerifier(paths[:1])
	if err != nil {
		return nil, err
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		combined := all(host, remote, key)
		var mismatch *knownhosts.KeyError
		if combined != nil && !errors.As(combined, &mismatch) {
			return combined
		}
		local := app(host, remote, key)
		if local == nil {
			return nil
		}
		var localMismatch *knownhosts.KeyError
		if errors.As(local, &localMismatch) && len(localMismatch.Want) == 0 {
			return combined
		}
		return local
	}, nil
}
