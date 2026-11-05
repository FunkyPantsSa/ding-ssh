package sshx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type HostTrustChallenge struct {
	ID              string   `json:"id"`
	Host            string   `json:"host"`
	Fingerprint     string   `json:"fingerprint"`
	KeyType         string   `json:"keyType"`
	Changed         bool     `json:"changed"`
	OldFingerprints []string `json:"oldFingerprints"`
	CanReplace      bool     `json:"canReplace"`
}
type pendingHostTrust struct {
	view     HostTrustChallenge
	key      ssh.PublicKey
	sources  []string
	snapshot string
	expires  time.Time
}

var challengeMu sync.Mutex
var challenges = map[string]pendingHostTrust{}

func hostTrustChallenge(host string, key ssh.PublicKey, sources []string, verification error) error {
	var keyError *knownhosts.KeyError
	if !errors.As(verification, &keyError) {
		return verification
	}
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return err
	}
	view := HostTrustChallenge{ID: hex.EncodeToString(bytes), Host: host, Fingerprint: ssh.FingerprintSHA256(key), KeyType: key.Type(), Changed: len(keyError.Want) > 0, CanReplace: true, OldFingerprints: []string{}}
	appFile := trustPaths(nil)[0]
	for _, want := range keyError.Want {
		view.OldFingerprints = append(view.OldFingerprints, ssh.FingerprintSHA256(want.Key))

	}
	data, err := os.ReadFile(appFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	challengeMu.Lock()
	defer challengeMu.Unlock()
	for id, pending := range challenges {
		if time.Now().After(pending.expires) {
			delete(challenges, id)
		}
	}
	if len(challenges) >= 256 {
		return fmt.Errorf("待确认主机过多，请稍后重试")
	}
	challenges[view.ID] = pendingHostTrust{view, key, append([]string{}, sources...), string(data), time.Now().Add(5 * time.Minute)}
	payload, _ := json.Marshal(view)
	return fmt.Errorf("主机身份需要确认 SSH_HOST_TRUST:%s", base64.RawURLEncoding.EncodeToString(payload))
}

// ResolveHostTrust consumes a backend-issued one-time challenge. Caller input
// cannot substitute the host or key. Explicit replacement writes only application
// trust; external files stay unchanged and revocations remain non-overridable.
func ResolveHostTrust(id string, approve, replace bool) error {
	challengeMu.Lock()
	pending, ok := challenges[id]
	delete(challenges, id)
	challengeMu.Unlock()
	if !ok || time.Now().After(pending.expires) {
		return fmt.Errorf("主机确认已过期，请重新连接")
	}
	if !approve {
		return nil
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	paths := trustPaths(nil)
	for _, source := range pending.sources {
		if _, err := os.Stat(expandSSHPath(source)); err != nil {
			return fmt.Errorf("读取指定信任源失败: %w", err)
		}
		paths = append(paths, expandSSHPath(source))
	}
	verify, err := preferredHostKeyVerifier(paths)
	if err != nil {
		return err
	}
	err = verify(pending.view.Host, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, pending.key)
	if err == nil {
		return nil
	}
	var mismatch *knownhosts.KeyError
	if !errors.As(err, &mismatch) {
		return err
	}
	file := trustPaths(nil)[0]
	data, readErr := os.ReadFile(file)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if string(data) != pending.snapshot {
		return fmt.Errorf("信任记录已变化，请重新连接并核对")
	}
	lines := strings.Split(string(data), "\n")
	if len(mismatch.Want) > 0 {
		expected := map[string]bool{}
		for _, fingerprint := range pending.view.OldFingerprints {
			expected[fingerprint] = true
		}
		if len(mismatch.Want) != len(pending.view.OldFingerprints) {
			return fmt.Errorf("信任记录已变化，请重新核对")
		}
		for _, want := range mismatch.Want {
			if !expected[ssh.FingerprintSHA256(want.Key)] {
				return fmt.Errorf("信任记录已变化，请重新核对")
			}
		}
		if !pending.view.Changed || !replace || !pending.view.CanReplace {
			return fmt.Errorf("未授权覆盖，或冲突来自只读信任源")
		}
		remove := map[int]bool{}
		for _, want := range mismatch.Want {
			if want.Filename != file {
				continue
			} // Preserve read-only OpenSSH sources.
			fields := strings.Fields(lines[want.Line-1])
			if len(fields) < 3 || strings.HasPrefix(fields[0], "@") || strings.Contains(fields[0], ",") {
				return fmt.Errorf("该记录包含特殊标记或多个主机，请在信任管理中处理")
			}
			remove[want.Line-1] = true
		}
		kept := []string{}
		for index, line := range lines {
			if !remove[index] {
				kept = append(kept, line)
			}
		}
		lines = kept
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if text != "" {
		text += "\n"
	}
	text += knownhosts.Line([]string{pending.view.Host}, pending.key) + "\n"
	return writeTrustFile(file, []byte(text))
}
