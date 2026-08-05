package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Session 表示一条已建立的 SSH 终端会话。
type Session struct {
	ID     string
	client *ssh.Client
	shell  *ssh.Session
	stdin  io.WriteCloser

	server    models.ServerNode
	createdAt int64

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
	onClosed  func(id string)
	onOutput  func(id string, data []byte)
	onStatus  func(id string, status models.SessionStatus, message string)

	sftpOnce sync.Once
	sftpErr  error
	sftp     *sftp.Client
}

// newSession 建立 SSH 连接并打开交互式 Shell。
func newSession(
	id string,
	node models.ServerNode,
	cols, rows int,
	onOutput func(string, []byte),
	onStatus func(string, models.SessionStatus, string),
	onClosed func(string),
	onProgress func(string, string),
) (*Session, error) {
	if onProgress != nil {
		onProgress(id, "正在初始化连接…")
	}
	config, err := buildClientConfig(node)
	if err != nil {
		return nil, err
	}

	if onProgress != nil {
		onProgress(id, "正在连接主机（TCP 与 SSH 握手）…")
	}
	client, err := dialSSH(node.Host, node.Port, config)
	if err != nil {
		return nil, err
	}
	logx.Debugf("SSH TCP 连接已建立: session=%s host=%s:%d", id, node.Host, node.Port)

	if onProgress != nil {
		onProgress(id, "SSH 握手成功，正在请求 PTY…")
	}

	shell, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("创建 SSH 会话失败: %w", err)
	}

	if onProgress != nil {
		onProgress(id, "PTY 已就绪，正在启动 Shell…")
	}

	// 分配 PTY，使用 256 色终端。
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		shell.Close()
		client.Close()
		return nil, fmt.Errorf("请求 PTY 失败: %w", err)
	}

	stdin, err := shell.StdinPipe()
	if err != nil {
		shell.Close()
		client.Close()
		return nil, fmt.Errorf("获取 stdin 失败: %w", err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		shell.Close()
		client.Close()
		return nil, fmt.Errorf("获取 stdout 失败: %w", err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		shell.Close()
		client.Close()
		return nil, fmt.Errorf("获取 stderr 失败: %w", err)
	}

	if err := shell.Shell(); err != nil {
		shell.Close()
		client.Close()
		return nil, fmt.Errorf("启动 Shell 失败: %w", err)
	}
	logx.Debugf("SSH Shell 已就绪: session=%s server=%s", id, node.Name)
	if onProgress != nil {
		onProgress(id, "Shell 已启动，连接完成")
	}

	s := &Session{
		ID:        id,
		client:    client,
		shell:     shell,
		stdin:     stdin,
		server:    node,
		createdAt: time.Now().UnixMilli(),
		onOutput:  onOutput,
		onStatus:  onStatus,
		onClosed:  onClosed,
	}

	go s.pump(stdout)
	go s.pump(stderr)

	// 等待 Shell 退出，统一触发清理。
	go func() {
		_ = shell.Wait()
		s.close(models.StatusClosed, "")
	}()

	return s, nil
}

// pump 持续读取输出流并转发为事件。
func (s *Session) pump(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if s.onOutput != nil {
				s.onOutput(s.ID, chunk)
			}
		}
		if err != nil {
			return
		}
	}
}

// Write 向前端写入终端输入。
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("会话已关闭")
	}
	_, err := s.stdin.Write(data)
	return err
}

// Resize 调整终端窗口尺寸。
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.shell.WindowChange(rows, cols)
}

// SftpClient 返回会话对应的 SFTP 客户端（按需懒创建，随会话关闭）。
func (s *Session) SftpClient() (*sftp.Client, error) {
	s.sftpOnce.Do(func() {
		s.sftp, s.sftpErr = sftp.NewClient(s.client)
	})
	return s.sftp, s.sftpErr
}

// Info 返回会话摘要信息。
func (s *Session) Info() models.SessionInfo {
	return models.SessionInfo{
		SessionID:  s.ID,
		ServerName: s.server.Name,
		Host:       s.server.Host,
		User:       s.server.User,
		Status:     models.StatusConnected,
		CreatedAt:  s.createdAt,
	}
}

func (s *Session) close(status models.SessionStatus, message string) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.closed = true
		s.mu.Unlock()

		logx.Debugf("SSH 会话关闭: session=%s status=%s message=%q", s.ID, status, message)
		if s.sftp != nil {
			_ = s.sftp.Close()
		}
		_ = s.shell.Close()
		_ = s.client.Close()

		if s.onStatus != nil {
			s.onStatus(s.ID, status, message)
		}
		if s.onClosed != nil {
			s.onClosed(s.ID)
		}
	})
}

// buildClientConfig 根据节点配置构造 SSH 客户端配置。
func buildClientConfig(node models.ServerNode) (*ssh.ClientConfig, error) {
	auths, err := buildAuthMethods(node)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            node.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO(Phase 4): 接入 known_hosts 校验
		Timeout:         0,
	}, nil
}

func buildAuthMethods(node models.ServerNode) ([]ssh.AuthMethod, error) {
	switch node.AuthType {
	case "privateKey":
		data, err := privateKeyData(node)
		if err != nil {
			return nil, err
		}
		key, err := parsePrivateKey(data, node.Password)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(key)}, nil
	default: // password
		return []ssh.AuthMethod{
			ssh.Password(node.Password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = node.Password
				}
				return answers, nil
			}),
		}, nil
	}
}

// connectTimeout SSH 连接（TCP 拨号 + 握手）超时时间。
const connectTimeout = 10 * time.Second

// dialSSH 建立 SSH 连接，TCP 拨号与握手合计 10 秒超时，超时返回明确提示。
func dialSSH(host string, port int, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, connectTimeout)
	if err != nil {
		return nil, connectError(err)
	}
	// 握手阶段同样受 10 秒 deadline 约束，避免服务端无响应时无限等待。
	if err := conn.SetDeadline(time.Now().Add(connectTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, connectError(err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// connectError 将超时类错误转换为明确的超时提示。
func connectError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("SSH 连接超时（10 秒）")
	}
	return fmt.Errorf("SSH 连接失败: %w", err)
}

// privateKeyData 返回私钥内容：优先使用粘贴的私钥内容，其次读取私钥文件。
func privateKeyData(node models.ServerNode) ([]byte, error) {
	if content := strings.TrimSpace(node.KeyContent); content != "" {
		return []byte(content), nil
	}
	if strings.TrimSpace(node.KeyPath) == "" {
		return nil, errors.New("请填写私钥内容或选择私钥文件")
	}
	return readKey(node.KeyPath)
}

// readKey 读取私钥文件内容。
func readKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件失败: %w", err)
	}
	return data, nil
}

func parsePrivateKey(data []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		key, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("解析加密私钥失败: %w", err)
		}
		return key, nil
	}
	key, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	return key, nil
}
