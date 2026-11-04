package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// loadHostKeyVerifier loads every configured trust source. Missing optional
// files are ignored; malformed or unreadable existing sources fail closed.
// This function never adds trust or overwrites a changed key.
func loadHostKeyVerifier(paths []string) (ssh.HostKeyCallback, error) {
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("读取主机信任库 %s: %w", path, err)
		}
		files = append(files, path)
	}
	verify, err := knownhosts.New(files...)
	if err != nil {
		return nil, fmt.Errorf("解析主机信任库: %w", err)
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(host, remote, key); err != nil {
			var mismatch *knownhosts.KeyError
			if errors.As(err, &mismatch) && len(mismatch.Want) > 0 {
				old := []string{}
				for _, want := range mismatch.Want {
					old = append(old, ssh.FingerprintSHA256(want.Key))
				}
				return fmt.Errorf("主机密钥发生变化，拒绝连接 %s；旧指纹：%s；新指纹：%s（可能存在中间人攻击）: %w", host, strings.Join(old, ", "), ssh.FingerprintSHA256(key), err)
			}
			return fmt.Errorf("主机身份验证失败 %s (%s %s): %w", host, key.Type(), ssh.FingerprintSHA256(key), err)
		}
		return nil
	}, nil
}
