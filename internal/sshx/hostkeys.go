package sshx

import (
	"ding-ssh/internal/i18n"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"ding-ssh/internal/logx"
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
			logx.Debugf("[sshx] 读取主机信任源失败: path=%q err=%v", path, err)
			return nil, fmt.Errorf(i18n.T("读取主机信任库 %s: %w"), path, err)
		}
		files = append(files, path)
	}
	verify, err := knownhosts.New(files...)
	if err != nil {
		logx.Debugf("[sshx] 解析主机信任库失败: sources=%d err=%v", len(files), err)
		return nil, fmt.Errorf(i18n.T("解析主机信任库: %w"), err)
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(host, remote, key); err != nil {
			var mismatch *knownhosts.KeyError
			if errors.As(err, &mismatch) && len(mismatch.Want) > 0 {
				old := []string{}
				for _, want := range mismatch.Want {
					old = append(old, ssh.FingerprintSHA256(want.Key))
				}
				newFP := ssh.FingerprintSHA256(key)
				logx.Warnf("[sshx] 主机密钥发生变化，拒绝连接: host=%s keyType=%s old=%s new=%s",
					host, key.Type(), strings.Join(old, ","), newFP)
				return fmt.Errorf(i18n.T("主机密钥发生变化，拒绝连接 %s；旧指纹：%s；新指纹：%s（可能存在中间人攻击）: %w"), host, strings.Join(old, ", "), newFP, err)
			}
			logx.Debugf("[sshx] 主机身份验证失败: host=%s keyType=%s fingerprint=%s err=%v", host, key.Type(), ssh.FingerprintSHA256(key), err)
			return fmt.Errorf(i18n.T("主机身份验证失败 %s (%s %s): %w"), host, key.Type(), ssh.FingerprintSHA256(key), err)
		}
		logx.Debugf("[sshx] 主机密钥校验通过: host=%s keyType=%s fingerprint=%s", host, key.Type(), ssh.FingerprintSHA256(key))
		return nil
	}, nil
}
