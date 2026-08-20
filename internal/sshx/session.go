package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
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
	onDirSync func(id string, path string)
	onProgress func(id, step, status, log, message string) // 连接过程分步日志
	lastDir   string // 最近一次同步的路径，用于去重避免重复事件

	sftpOnce sync.Once
	sftpErr  error
	sftp     *sftp.Client
	home     string // 远程 home，用于把提示符里的 ~ 展开成绝对路径

	keepAliveEnabled bool // 是否发送心跳包（由应用设置控制）
}

// noopProgress 无操作进度回调，用于无需展示连接过程日志的调用方（如隧道）。
func noopProgress(step, status, log, message string) {}

// progressFn 连接过程进度回调：
// step 为步骤标识（dns|tcp|auth|pty|ready），status 为 running|done|error，
// log 为追加的详细日志行，message 为步骤结束/失败时的摘要。
type progressFn func(step, status, log, message string)

// newSession 建立 SSH 连接并打开交互式 Shell。
func newSession(
	id string,
	node models.ServerNode,
	cols, rows int,
	onOutput func(string, []byte),
	onStatus func(string, models.SessionStatus, string),
	onClosed func(string),
	onDirSync func(string, string), // 终端 cwd 变化（绝对路径）
	onProgress func(string, string, string, string, string), // 连接进度分步日志
	keepAliveEnabled bool,
) (*Session, error) {
	report := func(step, status, log, message string) {
		if onProgress != nil {
			onProgress(id, step, status, log, message)
		}
	}
	report(models.ConnectStepDNS, "running", "正在初始化连接…", "")
	config, err := buildClientConfig(node, report)
	if err != nil {
		report(models.ConnectStepAuth, "error", fmt.Sprintf("构造鉴权配置失败：%v", err), err.Error())
		return nil, err
	}

	client, err := dialSSH(node.Host, node.Port, config, report)
	if err != nil {
		return nil, err
	}
	logx.Debugf("SSH TCP 连接已建立: session=%s host=%s:%d", id, node.Host, node.Port)

	// 分配 PTY，使用 256 色终端。
	report(models.ConnectStepPTY, "running", "正在创建 SSH 会话通道…", "")
	shell, err := client.NewSession()
	if err != nil {
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf("创建 SSH 会话失败：%v", err), "创建 SSH 会话失败")
		return nil, fmt.Errorf("创建 SSH 会话失败: %w", err)
	}

	report(models.ConnectStepPTY, "running", fmt.Sprintf("请求 PTY（xterm-256color %d×%d）…", cols, rows), "")
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf("请求 PTY 失败：%v", err), "请求 PTY 失败")
		return nil, fmt.Errorf("请求 PTY 失败: %w", err)
	}
	report(models.ConnectStepPTY, "done", "PTY 分配成功", "")

	report(models.ConnectStepReady, "running", "正在获取 IO 管道…", "")
	stdin, err := shell.StdinPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stdin 失败：%v", err), "获取 stdin 失败")
		return nil, fmt.Errorf("获取 stdin 失败: %w", err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stdout 失败：%v", err), "获取 stdout 失败")
		return nil, fmt.Errorf("获取 stdout 失败: %w", err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stderr 失败：%v", err), "获取 stderr 失败")
		return nil, fmt.Errorf("获取 stderr 失败: %w", err)
	}

	report(models.ConnectStepReady, "running", "正在启动 Shell…", "")
	if err := shell.Shell(); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("启动 Shell 失败：%v", err), "启动 Shell 失败")
		return nil, fmt.Errorf("启动 Shell 失败: %w", err)
	}
	logx.Debugf("SSH Shell 已就绪: session=%s server=%s", id, node.Name)
	report(models.ConnectStepReady, "done", "Shell 已启动，会话就绪", "")

	s := &Session{
		ID:               id,
		client:           client,
		shell:            shell,
		stdin:            stdin,
		server:           node,
		createdAt:        time.Now().UnixMilli(),
		onOutput:         onOutput,
		onStatus:         onStatus,
		onClosed:         onClosed,
		onDirSync:        onDirSync,
		onProgress:       onProgress,
		keepAliveEnabled: keepAliveEnabled,
	}

	// 启动 SSH KeepAlive 心跳 Goroutine
	go s.keepAliveLoop()

	go s.pump(stdout)
	go s.pump(stderr)
	go s.prefetchHomeAndSync()

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
	acc := make([]byte, 0, 64*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if s.onOutput != nil {
				s.onOutput(s.ID, chunk)
			}
			acc = append(acc, chunk...)
			if len(acc) > 64*1024 {
				acc = acc[len(acc)-64*1024:]
			}
			if s.onDirSync != nil {
				if dir, src, ok := ParseDirFromOutput(acc); ok {
					if dir = s.expandPath(dir); dir != "" {
						s.mu.Lock()
						same := dir == s.lastDir
						s.lastDir = dir
						s.mu.Unlock()
						if same {
							if containsNewline(chunk) {
								logx.Debugf("目录同步跳过(未变化): session=%s path=%s src=%s", s.ID, dir, src)
							}
						} else {
							logx.Infof("目录同步: session=%s path=%s src=%s", s.ID, dir, src)
							s.onDirSync(s.ID, dir)
						}
					}
				} else if containsNewline(chunk) {
					logx.Debugf("目录同步未解析: session=%s tail=%q", s.ID, previewTail(acc, 120))
				}
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

// guessHome 在尚未探测到远程 home 时给出常见默认值。
func (s *Session) guessHome() string {
	s.mu.Lock()
	if s.home != "" {
		h := s.home
		s.mu.Unlock()
		return h
	}
	s.mu.Unlock()
	u := s.server.User
	if u == "root" || u == "" {
		return "/root"
	}
	return "/home/" + u
}

// expandPath 把 ~ 展开为绝对路径；非绝对路径返回空。
func (s *Session) expandPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	if p == "~" {
		return path.Clean(s.guessHome())
	}
	if strings.HasPrefix(p, "~/") {
		return path.Join(s.guessHome(), p[2:])
	}
	return ""
}

// prefetchHomeAndSync 用 SFTP Getwd 校正 home，并在尚未同步过目录时推一次，
// 避免登录提示符是 ~ 时 SFTP 停在 /。
func (s *Session) prefetchHomeAndSync() {
	c, err := s.SftpClient()
	if err == nil && c != nil {
		if wd, werr := c.Getwd(); werr == nil && strings.HasPrefix(wd, "/") {
			s.mu.Lock()
			s.home = path.Clean(wd)
			s.mu.Unlock()
		}
	}
	home := s.guessHome()
	if home == "" || s.onDirSync == nil {
		return
	}
	s.mu.Lock()
	if s.lastDir != "" {
		s.mu.Unlock()
		return
	}
	s.lastDir = home
	s.mu.Unlock()
	logx.Infof("目录同步: session=%s path=%s src=sftp-home", s.ID, home)
	s.onDirSync(s.ID, home)
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
		// disconnected 状态保留会话在 Manager 中，支持一键重新连接
		if s.onClosed != nil && status != models.StatusDisconnected {
			s.onClosed(s.ID)
		}
	})
}

// buildClientConfig 根据节点配置构造 SSH 客户端配置。
// 在鉴权步骤中通过 HostKeyCallback / BannerCallback / AuthCallback 上报详细日志。
func buildClientConfig(node models.ServerNode, report progressFn) (*ssh.ClientConfig, error) {
	auths, err := buildAuthMethods(node)
	if err != nil {
		return nil, err
	}
	if node.AuthType == "privateKey" {
		report(models.ConnectStepAuth, "running", "鉴权配置：私钥认证", "")
	} else {
		report(models.ConnectStepAuth, "running", "鉴权配置：密码认证（含键盘交互）", "")
	}
	return &ssh.ClientConfig{
		User: node.User,
		Auth: auths,
		HostKeyCallback: func(_ string, remote net.Addr, key ssh.PublicKey) error {
			report(models.ConnectStepAuth, "running",
				fmt.Sprintf("服务端主机密钥：%s 指纹 sha256:%s", key.Type(), ssh.FingerprintSHA256(key)), "")
			return nil // TODO(Phase 4): 接入 known_hosts 校验
		},
		BannerCallback: func(message string) error {
			report(models.ConnectStepAuth, "running", fmt.Sprintf("服务端横幅：%s", message), "")
			return nil
		},
		AuthCallback: func(ctx *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			if len(ctx.AllowedMethods) > 0 {
				report(models.ConnectStepAuth, "running",
					fmt.Sprintf("服务器允许的鉴权方式：%s", strings.Join(ctx.AllowedMethods, ", ")), "")
			}
			if len(ctx.TriedMethods) > 0 {
				report(models.ConnectStepAuth, "running",
					fmt.Sprintf("已失败的鉴权尝试：%s", strings.Join(ctx.TriedMethods, ", ")), "")
			}
			return nil, nil
		},
		Timeout: 0,
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

// dialSSH 建立 SSH 连接，按 DNS / TCP / 鉴权三步上报详细日志，
// TCP 拨号与握手合计 10 秒超时，超时返回明确提示。
func dialSSH(host string, port int, config *ssh.ClientConfig, report progressFn) (*ssh.Client, error) {
	// 步骤 1：DNS / 直连。
	report(models.ConnectStepDNS, "running", fmt.Sprintf("目标主机：%s", host), "")
	if ip := net.ParseIP(host); ip != nil {
		report(models.ConnectStepDNS, "running", fmt.Sprintf("主机为 IP 地址，采用直连（%s）", ip), "")
	} else {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			msg := "DNS 解析失败"
			report(models.ConnectStepDNS, "error", fmt.Sprintf("解析 %s 失败：%v", host, err), msg)
			return nil, fmt.Errorf("%s: %w", msg, err)
		}
		addrs := make([]string, 0, len(ips))
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
		report(models.ConnectStepDNS, "running", fmt.Sprintf("DNS 解析成功：%s", strings.Join(addrs, ", ")), "")
	}
	report(models.ConnectStepDNS, "done", fmt.Sprintf("目标地址已确定：%s:%d", host, port), "")

	// 步骤 2：TCP 握手。
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	report(models.ConnectStepTCP, "running", fmt.Sprintf("正在建立 TCP 连接 %s …", addr), "")
	dialStart := time.Now()
	conn, err := net.DialTimeout("tcp", addr, connectTimeout)
	if err != nil {
		cerr := connectError(err)
		report(models.ConnectStepTCP, "error", fmt.Sprintf("TCP 连接失败：%v", err), cerr.Error())
		return nil, cerr
	}
	report(models.ConnectStepTCP, "done", fmt.Sprintf("TCP 连接成功，耗时 %dms，对端 %s",
		time.Since(dialStart).Milliseconds(), conn.RemoteAddr().String()), "")

	// 步骤 3：SSH 鉴权（版本协商 + 密钥交换 + 用户鉴权）。
	// 握手阶段受 10 秒 deadline 约束，避免服务端无响应时无限等待。
	if err := conn.SetDeadline(time.Now().Add(connectTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	report(models.ConnectStepAuth, "running", "开始 SSH 版本协商与密钥交换…", "")
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		cerr := connectError(err)
		report(models.ConnectStepAuth, "error", fmt.Sprintf("SSH 握手失败：%v", err), cerr.Error())
		return nil, cerr
	}
	report(models.ConnectStepAuth, "running", fmt.Sprintf("服务端版本：%s", sshConn.ServerVersion()), "")
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	report(models.ConnectStepAuth, "done", fmt.Sprintf("SSH 鉴权成功（用户 %s）", config.User), "")
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

// keepAliveLoop 定期发送 SSH keepalive 心跳包，防止 NAT 或防火墙超时断开。
// 当 keepAliveEnabled 为 false 时仅发送空请求保持终端活跃，不做失败检测。
// 连续失败立即触发连接断开事件，前端保留终端上下文并提供重新连接入口。
func (s *Session) keepAliveLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		closed := s.closed
		client := s.client
		kaEnabled := s.keepAliveEnabled
		s.mu.Unlock()
		if closed || client == nil {
			return
		}
		if !kaEnabled {
			// 心跳关闭时仍发送 no-op 请求防止服务端终端超时，但不处理错误
			_, _, _ = client.SendRequest("keepalive@openssh.com", false, nil)
			continue
		}
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			logx.Debugf("SSH keepalive 失败，触发断开事件: session=%s err=%v", s.ID, err)
			s.close(models.StatusDisconnected, "连接已断开（心跳超时）")
			return
		}
	}
}

// SetKeepAliveEnabled 动态更新心跳检测开关（不影响终端 no-op 心跳）。
func (s *Session) SetKeepAliveEnabled(enabled bool) {
	s.mu.Lock()
	s.keepAliveEnabled = enabled
	s.mu.Unlock()
}

// Reconnect 重新建立 SSH 连接，复用原会话配置。
// 在心跳断开 (disconnected) 或异常断开 (error) 后调用，保留原有的 onOutput/onStatus 回调。
// keepAliveEnabled 用于更新心跳开关设置。
func (s *Session) Reconnect(cols, rows int, keepAliveEnabled bool) error {
	s.mu.Lock()
	wasClosed := s.closed
	if !wasClosed {
		s.mu.Unlock()
		return fmt.Errorf("会话仍在连接中，无需重连")
	}
	// 重置关闭状态，复用原会话 ID 和回调
	s.closed = false
	s.closeOnce = sync.Once{}
	server := s.server
	onStatus := s.onStatus
	onProgress := s.onProgress
	s.mu.Unlock()

	report := func(step, status, log, message string) {
		if onProgress != nil {
			onProgress(s.ID, step, status, log, message)
		}
	}
	report(models.ConnectStepDNS, "running", "正在重新连接…", "")

	// 如果旧连接未完全关闭，先清理
	if s.client != nil {
		_ = s.client.Close()
	}
	if s.sftp != nil {
		_ = s.sftp.Close()
		s.sftp = nil
	}
	s.sftpOnce = sync.Once{}
	s.sftpErr = nil

	config, err := buildClientConfig(server, report)
	if err != nil {
		report(models.ConnectStepAuth, "error", fmt.Sprintf("构造鉴权配置失败：%v", err), err.Error())
		return err
	}

	client, err := dialSSH(server.Host, server.Port, config, report)
	if err != nil {
		return err
	}

	report(models.ConnectStepPTY, "running", "正在创建 SSH 会话通道…", "")
	shell, err := client.NewSession()
	if err != nil {
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf("创建 SSH 会话失败：%v", err), "创建 SSH 会话失败")
		return fmt.Errorf("创建 SSH 会话失败: %w", err)
	}

	report(models.ConnectStepPTY, "running", fmt.Sprintf("请求 PTY（xterm-256color %d×%d）…", cols, rows), "")
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf("请求 PTY 失败：%v", err), "请求 PTY 失败")
		return fmt.Errorf("请求 PTY 失败: %w", err)
	}
	report(models.ConnectStepPTY, "done", "PTY 分配成功", "")

	report(models.ConnectStepReady, "running", "正在获取 IO 管道…", "")
	stdin, err := shell.StdinPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stdin 失败：%v", err), "获取 stdin 失败")
		return fmt.Errorf("获取 stdin 失败: %w", err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stdout 失败：%v", err), "获取 stdout 失败")
		return fmt.Errorf("获取 stdout 失败: %w", err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("获取 stderr 失败：%v", err), "获取 stderr 失败")
		return fmt.Errorf("获取 stderr 失败: %w", err)
	}

	report(models.ConnectStepReady, "running", "正在启动 Shell…", "")
	if err := shell.Shell(); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf("启动 Shell 失败：%v", err), "启动 Shell 失败")
		return fmt.Errorf("启动 Shell 失败: %w", err)
	}
	report(models.ConnectStepReady, "done", "Shell 已启动，会话就绪", "")

	s.mu.Lock()
	s.client = client
	s.shell = shell
	s.stdin = stdin
	s.closed = false
	s.keepAliveEnabled = keepAliveEnabled
	s.mu.Unlock()

	go s.keepAliveLoop()
	go s.pump(stdout)
	go s.pump(stderr)

	go func() {
		_ = shell.Wait()
		s.close(models.StatusClosed, "")
	}()

	if onStatus != nil {
		onStatus(s.ID, models.StatusConnected, "已重新连接")
	}

	return nil
}
