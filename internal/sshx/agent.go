package sshx

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ding-ssh/internal/models"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func expandSSHPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimLeft(path[1:], "/\\"))
		}
	}
	return os.ExpandEnv(path)
}

func agentSocket(node models.ServerNode) (string, error) {
	socket := node.IdentityAgent
	if socket == "none" {
		return "", fmt.Errorf("IdentityAgent 禁用了 SSH Agent")
	}
	if socket == "" || socket == "SSH_AUTH_SOCK" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	}
	if socket == "" {
		socket = defaultAgentSocket()
	}
	if socket == "" {
		return "", fmt.Errorf("SSH Agent 不可用：SSH_AUTH_SOCK 未设置")
	}
	return expandSSHPath(socket), nil
}

func openAgent(socket string) (net.Conn, error) {
	conn, err := dialAgent(socket)
	if err != nil {
		return nil, fmt.Errorf("连接 SSH Agent 失败: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(connectTimeout))
	return &boundedAgentConn{Conn: conn, timer: time.AfterFunc(connectTimeout, func() { conn.Close() })}, nil
}

// AgentSigners exposes public identities only. Each signing request uses a
// bounded connection; no agent connection survives authentication.
func AgentSigners(node models.ServerNode) ([]ssh.Signer, error) {
	socket, err := agentSocket(node)
	if err != nil {
		return nil, err
	}
	conn, err := openAgent(socket)
	if err != nil {
		return nil, err
	}
	keys, err := agent.NewClient(conn).List()
	conn.Close()
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	if node.IdentitiesOnly {
		paths := append([]string{}, node.IdentityFiles...)
		if node.KeyPath != "" {
			paths = append(paths, node.KeyPath)
		}
		for _, path := range paths {
			path = expandSSHPath(path)
			data, readErr := os.ReadFile(path + ".pub")
			if readErr != nil {
				data, readErr = os.ReadFile(path)
			}
			if readErr != nil {
				return nil, fmt.Errorf("读取身份公钥 %s: %w", path, readErr)
			}
			pub, _, _, _, parseErr := ssh.ParseAuthorizedKey(data)
			if parseErr != nil {
				signer, e := ssh.ParsePrivateKey(data)
				if e != nil {
					var missing *ssh.PassphraseMissingError
					if errors.As(e, &missing) && missing.PublicKey != nil {
						pub = missing.PublicKey
					} else {
						return nil, fmt.Errorf("IdentitiesOnly 需要 %s.pub 公钥文件", path)
					}
				} else {
					pub = signer.PublicKey()
				}
			}
			allowed[string(pub.Marshal())] = true
		}
	}
	result := []ssh.Signer{}
	for _, key := range keys {
		pub, err := ssh.ParsePublicKey(key.Blob)
		if err != nil {
			return nil, err
		}
		if node.IdentitiesOnly && !allowed[string(pub.Marshal())] {
			continue
		}
		result = append(result, &agentIdentity{socket: socket, key: pub})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("SSH Agent 中没有匹配的身份")
	}
	return result, nil
}

type agentIdentity struct {
	socket string
	key    ssh.PublicKey
}

func (s *agentIdentity) PublicKey() ssh.PublicKey { return s.key }
func (s *agentIdentity) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	return s.SignWithAlgorithm(nil, data, "")
}
func (s *agentIdentity) SignWithAlgorithm(_ io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	conn, err := openAgent(s.socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	flags := agent.SignatureFlags(0)
	switch algorithm {
	case ssh.KeyAlgoRSASHA256:
		flags = agent.SignatureFlagRsaSha256
	case ssh.KeyAlgoRSASHA512:
		flags = agent.SignatureFlagRsaSha512
	}
	return agent.NewClient(conn).SignWithFlags(s.key, data, flags)
}

type boundedAgentConn struct {
	net.Conn
	timer *time.Timer
}

func (c *boundedAgentConn) Close() error { c.timer.Stop(); return c.Conn.Close() }

func autoSigners(node models.ServerNode) ([]ssh.Signer, error) {
	signers, agentErr := AgentSigners(node)
	if node.IdentityAgent == "none" {
		signers = nil
	}
	seen := map[string]bool{}
	for _, signer := range signers {
		seen[string(signer.PublicKey().Marshal())] = true
	}
	for _, path := range node.IdentityFiles {
		data, err := os.ReadFile(expandSSHPath(path))
		if err != nil {
			if len(signers) == 0 {
				return nil, fmt.Errorf("读取身份文件 %s: %w", path, err)
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if missing.PublicKey != nil && seen[string(missing.PublicKey.Marshal())] {
				continue
			}
			phrase := node.RuntimePassphrases[path]
			if phrase == "" {
				return nil, fmt.Errorf("加密私钥需要密码短语 SSH_KEY_PASSPHRASE:%s", base64.RawURLEncoding.EncodeToString([]byte(path)))
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(phrase))
		}
		if err != nil {
			return nil, fmt.Errorf("解析身份文件 %s: %w", path, err)
		}
		if !seen[string(signer.PublicKey().Marshal())] {
			signers = append(signers, signer)
			seen[string(signer.PublicKey().Marshal())] = true
		}
	}
	if len(signers) == 0 {
		if agentErr != nil {
			return nil, agentErr
		}
		return nil, fmt.Errorf("没有可用的 SSH 身份")
	}
	return signers, nil
}
