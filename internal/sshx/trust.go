package sshx

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ding-ssh/internal/appdir"
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
	for _, source := range extra {
		if _, err := os.Stat(expandSSHPath(source)); err != nil {
			return fmt.Errorf("读取指定信任源失败: %w", err)
		}
	}
	paths := trustPaths(nil)
	for _, source := range extra {
		paths = append(paths, expandSSHPath(source))
	}
	verify, err := loadHostKeyVerifier(paths)
	if err != nil {
		return err
	}
	return verify(host, remote, key)
}

func ListHostTrust() ([]HostTrustRecord, error) {
	trustMu.Lock()
	defer trustMu.Unlock()
	result := []HostTrustRecord{}
	for i, file := range trustPaths(nil) {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
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
				return nil, fmt.Errorf("无效信任记录 %s:%d", file, line)
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
	}
	return result, nil
}

// SaveHostTrust only adds an explicitly approved public key, refusing existing
// conflicts and revocations across all default sources.
func SaveHostTrust(host, authorizedKey string) error {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return err
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	verify, err := loadHostKeyVerifier(trustPaths(nil))
	if err != nil {
		return err
	}
	err = verify(host, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key)
	if err == nil {
		return nil
	}
	var unknown *knownhosts.KeyError
	if !errors.As(err, &unknown) || len(unknown.Want) > 0 {
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
	return writeTrustFile(file, data)
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
		return fmt.Errorf("记录已变化，请刷新")
	}
	fields := strings.Fields(lines[line-1])
	if len(fields) < 3 {
		return fmt.Errorf("无效记录")
	}
	if strings.HasPrefix(fields[0], "@") {
		return fmt.Errorf("撤销记录不能从此界面删除")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
	if err != nil || ssh.FingerprintSHA256(key) != fingerprint {
		return fmt.Errorf("记录已变化，请刷新")
	}
	lines = append(lines[:line-1], lines[line:]...)
	return writeTrustFile(file, []byte(strings.Join(lines, "\n")))
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
	var unknown *knownhosts.KeyError
	if errors.As(err, &unknown) && len(unknown.Want) == 0 {
		payload, _ := json.Marshal(map[string]string{"host": host, "key": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), "fingerprint": ssh.FingerprintSHA256(key)})
		return fmt.Errorf("未知主机，需要核对指纹并明确授权 SSH_HOST_TRUST:%s", base64.RawURLEncoding.EncodeToString(payload))
	}
	return err
}
