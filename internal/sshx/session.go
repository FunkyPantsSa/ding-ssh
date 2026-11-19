package sshx

import (
	"context"
	"ding-ssh/internal/i18n"
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

// dirLineCarryMax 限制"尚未成行"尾部的最大保留长度，避免没有换行的巨量输出占内存。
// 正常的一行终端输出远小于该值；超长行会被截断，其中的 cd 回显不再解析。
const dirLineCarryMax = 4 * 1024

const (
	// dirJobQueue 目录同步队列长度。目录事件只在路径真正变化时入队，正常远小于该值。
	dirJobQueue = 64
	// dirVerifyTimeout 单次远端目录存在性校验的超时；超时按"无法判断"处理，不阻塞终端。
	dirVerifyTimeout = 800 * time.Millisecond
	// dirVerifyCooldown 校验超时后的冷却时间，避免 SFTP 卡住时为每次 cd 反复起协程。
	dirVerifyCooldown = 30 * time.Second
)

// dirCandidate 解析出的目录变更候选：src 为 "cd" 时 value 是回显参数（可能是相对路径），
// 其余 src 的 value 已经是绝对目录（提示符 / OSC 7 / OSC 3008）。
type dirCandidate struct {
	value string
	src   string
}

// dirJob 交给串行同步协程处理的目录事件。gen 用于丢弃旧连接（含重连前）遗留的任务。
type dirJob struct {
	dirCandidate
	gen uint64
}

// Session 表示一条已建立的 SSH 终端会话。
type Session struct {
	ID     string
	client *ssh.Client
	shell  *ssh.Session
	stdin  io.WriteCloser

	server    models.ServerNode
	createdAt int64

	mu           sync.Mutex
	writeMu      sync.Mutex
	closed       bool
	closing      bool
	reconnecting bool
	onClosed     func(id string)
	onOutput     func(id string, data []byte)
	onStatus     func(id string, status models.SessionStatus, message string)
	onDirSync    func(id string, path string)
	onProgress   func(id, step, status, log, message string) // 连接过程分步日志
	lastDir      string                                      // 最近一次同步的路径，用于去重避免重复事件

	// 终端目录同步：解析结果入队，由单个协程串行处理（必要时做远端存在性校验），
	// 避免 SFTP 请求阻塞终端输出读取，也保证相对 cd 按顺序解析。
	dirJobs   chan dirJob
	dirStop   chan struct{}
	dirGen    uint64                                // 每次启动/停止同步协程递增；旧任务据此丢弃
	dirSignal string                                // 最近一次入队的绝对目录，用于抑制提示符重复入队
	verifyDir func(dir string) (exists, known bool) // 远端目录校验，测试可替换

	dirVerifyMu         sync.Mutex
	dirVerifyQuietUntil time.Time // 校验超时后的冷却截止时间
	dirVerifyBroken     bool      // SFTP 与 Shell 目录视图不一致，停用校验

	sftpState *sessionSFTP
	home      string // 远程 home，用于把提示符里的 ~ 展开成绝对路径

	keepAliveEnabled bool // 是否发送心跳包（由应用设置控制）
}

type sessionSFTP struct {
	once   sync.Once
	client *sftp.Client
	err    error
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
	contexts ...context.Context,
) (*Session, error) {
	report := func(step, status, log, message string) {
		if onProgress != nil {
			onProgress(id, step, status, log, message)
		}
	}
	report(models.ConnectStepDNS, "running", i18n.T("正在初始化连接…"), "")
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	client, err := dialNodeContext(ctx, node, report)
	if err != nil {
		return nil, err
	}
	stopSetupCancel := context.AfterFunc(ctx, func() { client.Close() })
	defer stopSetupCancel()
	logx.Debugf("SSH TCP 连接已建立: session=%s host=%s:%d", id, node.Host, node.Port)

	// 分配 PTY，使用 256 色终端。
	report(models.ConnectStepPTY, "running", i18n.T("正在创建 SSH 会话通道…"), "")
	shell, err := client.NewSession()
	if err != nil {
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf(i18n.T("创建 SSH 会话失败：%v"), err), i18n.T("创建 SSH 会话失败"))
		return nil, fmt.Errorf(i18n.T("创建 SSH 会话失败: %w"), err)
	}

	report(models.ConnectStepPTY, "running", fmt.Sprintf(i18n.T("请求 PTY（xterm-256color %d×%d）…"), cols, rows), "")
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf(i18n.T("请求 PTY 失败：%v"), err), i18n.T("请求 PTY 失败"))
		return nil, fmt.Errorf(i18n.T("请求 PTY 失败: %w"), err)
	}
	report(models.ConnectStepPTY, "done", i18n.T("PTY 分配成功"), "")

	report(models.ConnectStepReady, "running", i18n.T("正在获取 IO 管道…"), "")
	stdin, err := shell.StdinPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stdin 失败：%v"), err), i18n.T("获取 stdin 失败"))
		return nil, fmt.Errorf(i18n.T("获取 stdin 失败: %w"), err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stdout 失败：%v"), err), i18n.T("获取 stdout 失败"))
		return nil, fmt.Errorf(i18n.T("获取 stdout 失败: %w"), err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stderr 失败：%v"), err), i18n.T("获取 stderr 失败"))
		return nil, fmt.Errorf(i18n.T("获取 stderr 失败: %w"), err)
	}

	report(models.ConnectStepReady, "running", i18n.T("正在启动 Shell…"), "")
	if err := shell.Shell(); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("启动 Shell 失败：%v"), err), i18n.T("启动 Shell 失败"))
		return nil, fmt.Errorf(i18n.T("启动 Shell 失败: %w"), err)
	}
	logx.Debugf("SSH Shell 已就绪: session=%s server=%s", id, node.Name)
	report(models.ConnectStepReady, "done", i18n.T("Shell 已启动，会话就绪"), "")

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
		sftpState:        &sessionSFTP{},
	}

	// 启动 SSH KeepAlive 心跳 Goroutine
	go s.keepAliveLoop(client)

	// 目录同步解析结果串行处理（含 SFTP 存在性校验），与终端输出读取解耦。
	if onDirSync != nil && !node.JumpServerMode {
		s.startDirSync()
	}

	go s.pump(stdout)
	go s.pump(stderr)
	go s.prefetchHomeAndSync()

	// 等待 Shell 退出，统一触发清理。
	go func() {
		_ = shell.Wait()
		s.closeClient(client, models.StatusClosed, "")
	}()

	return s, nil
}

// pump 持续读取输出流并转发为事件。
func (s *Session) pump(r io.Reader) {
	buf := make([]byte, 32*1024)
	acc := make([]byte, 0, 64*1024)
	var carry []byte // 尚未成行的尾部：同一条输出行被拆成多次 Read 时仍只解析一次
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
			if s.onDirSync != nil && !s.server.JumpServerMode {
				candidates, next := s.resolveDirChange(acc, carry, chunk)
				carry = next
				for _, c := range candidates {
					s.enqueueDir(c)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// resolveDirChange 从一块新到达的终端输出中解析目录变更候选。
//   - cd 回显只在"本次补齐的完整行"里匹配，每条回显最多消费一次，避免历史回显被反复叠加；
//   - 提示符 / OSC 7 / OSC 3008 给出的都是绝对路径，重复解析结果稳定，仍在滚动缓冲上匹配；
//   - 优先级与 ParseDirFromOutput 一致：提示符 > cd > OSC 7 > OSC 3008。
//
// cd 候选只返回回显参数（可能是相对路径），由串行同步协程按"执行该命令前"的目录补全，
// 这样即使存在待校验的历史 cd，顺序也不会错。carry 是上一块未成行的尾部。
func (s *Session) resolveDirChange(acc, carry, chunk []byte) ([]dirCandidate, []byte) {
	lines, nextCarry := splitOutputLines(carry, chunk)

	if p, matched := ParsePrompt(acc); matched {
		if d := s.expandPath(p); d != "" {
			return []dirCandidate{{value: d, src: "prompt"}}, nextCarry
		}
	}

	var cds []dirCandidate
	for _, line := range lines {
		if p, matched := parseCdLine(line); matched {
			cds = append(cds, dirCandidate{value: p, src: "cd"})
		}
	}
	if len(cds) > 0 {
		return cds, nextCarry
	}
	if p, matched := ParseOSC7(acc); matched {
		if d := s.expandPath(p); d != "" {
			return []dirCandidate{{value: d, src: "osc7"}}, nextCarry
		}
	}
	if p, matched := ParseOSC3008(acc); matched {
		if d := s.expandPath(p); d != "" {
			return []dirCandidate{{value: d, src: "osc3008"}}, nextCarry
		}
	}
	return nil, nextCarry
}

// splitOutputLines 按换行把新输出拆成完整行，返回这些行与尚未成行的尾部。
// 每条完整行只会返回一次 —— 这是"cd 回显不被重复消费"的关键。
func splitOutputLines(carry, chunk []byte) ([][]byte, []byte) {
	if len(chunk) == 0 {
		return nil, carry
	}
	data := make([]byte, 0, len(carry)+len(chunk))
	data = append(data, carry...)
	data = append(data, chunk...)

	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' && data[i] != '\r' {
			continue
		}
		if i > start {
			lines = append(lines, data[start:i])
		}
		start = i + 1
	}
	rest := data[start:]
	if len(rest) > dirLineCarryMax {
		rest = rest[len(rest)-dirLineCarryMax:]
	}
	return lines, rest
}

// startDirSync 启动串行目录同步协程（新建会话与重连都会调用）。
// 解析结果集中到一个协程处理，终端输出读取不会因为 SFTP 校验而阻塞。
func (s *Session) startDirSync() {
	s.mu.Lock()
	if s.dirJobs != nil {
		s.mu.Unlock()
		return
	}
	s.dirGen++
	s.dirSignal = ""
	jobs := make(chan dirJob, dirJobQueue)
	stop := make(chan struct{})
	s.dirJobs, s.dirStop = jobs, stop
	s.mu.Unlock()

	s.dirVerifyMu.Lock()
	s.dirVerifyQuietUntil = time.Time{}
	s.dirVerifyBroken = false
	s.dirVerifyMu.Unlock()

	go s.dirSyncLoop(jobs, stop)
}

// stopDirSync 停止同步协程并丢弃尚未处理的任务（关闭 / 重连时调用）。
func (s *Session) stopDirSync() {
	s.mu.Lock()
	stop := s.dirStop
	if stop == nil {
		s.mu.Unlock()
		return
	}
	s.dirGen++ // 让正在校验或已出队的旧任务失效
	s.dirJobs, s.dirStop = nil, nil
	s.mu.Unlock()
	close(stop)
}

func (s *Session) dirSyncLoop(jobs <-chan dirJob, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case job := <-jobs:
			s.processDirJob(job)
		}
	}
}

// enqueueDir 把候选目录交给同步协程。提示符 / OSC 的绝对目录连续重复时不再入队，
// 避免每个输出块都触发一次；队列满时丢弃并记日志（目录事件只在路径变化时产生，正常不会发生）。
func (s *Session) enqueueDir(c dirCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirJobs == nil {
		return
	}
	if c.src != "cd" && c.value == s.dirSignal {
		return
	}
	select {
	case s.dirJobs <- dirJob{dirCandidate: c, gen: s.dirGen}:
		// 只有真正入队后才记录，避免队列满丢事件后把同样的提示符永久抑制掉。
		if c.src != "cd" {
			s.dirSignal = c.value
		}
	default:
		logx.Debugf("目录同步队列已满，丢弃: session=%s path=%s src=%s", s.ID, c.value, c.src)
	}
}

// processDirJob 串行处理一条目录事件：补全路径、校验目标是否真实存在、去重上报。
// cd 回显可能来自失败的命令（目录不存在 / 是文件），只按回显上报会把 SFTP 面板带到
// 不存在的路径；因此上报前用 SFTP Stat 校验一次。校验不可用（SFTP 被禁用、超时等）
// 时退回旧行为直接上报，保证目录同步本身仍可用。
func (s *Session) processDirJob(job dirJob) {
	if !s.dirJobValid(job.gen) {
		return
	}
	if job.src != "cd" {
		s.reportDir(job.value, job.src)
		return
	}
	dir := s.expandPath(job.value)
	if dir == "" {
		return
	}
	if exists, known := s.remoteDirExists(dir); known && !exists {
		// 有些部署的 SFTP 根目录与 Shell 不同（如 chroot 的 sftp-server）：目标在 SFTP 视角
		// "不存在"并不代表 cd 失败。当前目录也 Stat 不到时判定为视图不一致，停用校验。
		if s.sftpViewMismatch(dir) {
			s.disableDirVerify()
			logx.Debugf("SFTP 与 Shell 目录视图不一致，停用存在性校验: session=%s", s.ID)
			if s.dirJobValid(job.gen) {
				s.reportDir(dir, "cd")
			}
			return
		}
		logx.Debugf("目录同步忽略不可进入的目标: session=%s path=%s", s.ID, dir)
		return
	}
	if !s.dirJobValid(job.gen) {
		return
	}
	s.reportDir(dir, "cd")
}

// dirJobValid 判断任务是否仍属于当前连接：重连 / 关闭后旧任务（含校验中的）应被丢弃。
func (s *Session) dirJobValid(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirGen == gen && !s.closed && !s.closing
}

// sftpViewMismatch 判断"目标不存在"是否源于 SFTP 与 Shell 的目录视图差异：
// 若当前目录本身在 SFTP 视角也 Stat 不到，则目标路径不可信，调用方应停用校验。
func (s *Session) sftpViewMismatch(dir string) bool {
	base := s.currentDir()
	if base == "" || base == dir {
		return false
	}
	exists, known := s.remoteDirExists(base)
	return known && !exists
}

func (s *Session) disableDirVerify() {
	s.dirVerifyMu.Lock()
	s.dirVerifyBroken = true
	s.dirVerifyMu.Unlock()
}

// remoteDirExists 通过 SFTP 判断目录是否存在且确实是目录。
// exists 为结果，known 表示是否得到了确定结论；known=false 时调用方应退回乐观上报。
func (s *Session) remoteDirExists(dir string) (exists, known bool) {
	s.dirVerifyMu.Lock()
	skip := s.dirVerifyBroken || time.Now().Before(s.dirVerifyQuietUntil)
	s.dirVerifyMu.Unlock()
	if skip {
		return false, false
	}
	if s.verifyDir != nil {
		return s.verifyDir(dir)
	}
	c, err := s.SftpClient()
	if err != nil || c == nil {
		return false, false
	}

	type statResult struct {
		fi  os.FileInfo
		err error
	}
	done := make(chan statResult, 1)
	go func() {
		fi, err := c.Stat(dir)
		done <- statResult{fi: fi, err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			if errors.Is(r.err, os.ErrNotExist) {
				return false, true
			}
			return false, false
		}
		return r.fi.IsDir(), true
	case <-time.After(dirVerifyTimeout):
		s.dirVerifyMu.Lock()
		s.dirVerifyQuietUntil = time.Now().Add(dirVerifyCooldown)
		s.dirVerifyMu.Unlock()
		logx.Debugf("目录校验超时，暂时跳过校验: session=%s path=%s", s.ID, dir)
		return false, false
	}
}

// reportDir 上报目录变更：与上次相同时只记调试日志，避免重复推送。
func (s *Session) reportDir(dir, src string) {
	s.mu.Lock()
	same := dir == s.lastDir
	if !same {
		s.lastDir = dir
	}
	s.mu.Unlock()
	if same {
		logx.Debugf("目录同步跳过(未变化): session=%s path=%s src=%s", s.ID, dir, src)
		return
	}
	logx.Infof("目录同步: session=%s path=%s src=%s", s.ID, dir, src)
	s.onDirSync(s.ID, dir)
}

// Write 向前端写入终端输入。
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	stdin, closed := s.stdin, s.closed || s.closing
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("%s", i18n.T("会话已关闭"))
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := stdin.Write(data)
	return err
}

// Resize 调整终端窗口尺寸。
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	shell, closed := s.shell, s.closed || s.closing
	s.mu.Unlock()
	if closed {
		return nil
	}
	return shell.WindowChange(rows, cols)
}

// SftpClient 返回会话对应的 SFTP 客户端（按需懒创建，随 SSH 连接关闭）。
func (s *Session) SftpClient() (*sftp.Client, error) {
	s.mu.Lock()
	if s.closed || s.closing {
		s.mu.Unlock()
		return nil, fmt.Errorf("%s", i18n.T("会话已关闭"))
	}
	state, client := s.sftpState, s.client
	s.mu.Unlock()
	state.once.Do(func() {
		state.client, state.err = sftp.NewClient(client)
	})
	return state.client, state.err
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

// expandPath 把解析出的目录补全为绝对路径：
//   - 绝对路径 / 以 ~ 开头：直接规范化；
//   - 其余（提示符后 cd 回显的相对参数，如 ..、subdir）：基于最近一次已知目录 lastDir 解析。
//
// 只有 cd 回显会产生相对参数（prompt / osc7 / osc3008 给出的都是绝对路径或 ~），
// 每条回显只会被消费一次，因此这里按当时的目录拼接是安全的；解析不了时返回空串。
func (s *Session) expandPath(p string) string {
	return s.expandPathFrom(p, s.currentDir())
}

// expandPathFrom 以 base 为基准补全路径。相对参数必须用"该 cd 命令执行前"的目录解析；
// 流式场景由串行同步协程保证这一点，不要在一条回显上二次复用它更新后的目录。
func (s *Session) expandPathFrom(p, base string) string {
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
	// cd - 会回显 OLDPWD（非目标路径），cd . 指向当前目录，均不构成目录变更。
	if p == "-" || p == "" || p == "." {
		return ""
	}
	if base == "" {
		return ""
	}
	return path.Clean(path.Join(base, p))
}

// currentDir 返回最近一次同步（或探测）到的目录。
func (s *Session) currentDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDir
}

// prefetchHomeAndSync 用 SFTP Getwd 校正 home，并在尚未同步过目录时推一次，
// 避免登录提示符是 ~ 时 SFTP 停在 /。
func (s *Session) prefetchHomeAndSync() {
	if s.server.JumpServerMode {
		return
	}
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
	s.closeClient(nil, status, message)
}

func (s *Session) closeClient(expected *ssh.Client, status models.SessionStatus, message string) {
	s.mu.Lock()
	client := s.client
	if s.closed || s.closing || (expected != nil && expected != client) {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.mu.Unlock()

	// 先关闭传输，解除等待 SSH 通道读写和回复的操作。
	_ = client.Close()

	s.mu.Lock()
	s.closed = true
	s.closing = false
	s.mu.Unlock()

	// 停掉目录同步协程：连接已关，未处理的事件不再有参考价值。
	s.stopDirSync()

	logx.Debugf("SSH 会话关闭: session=%s status=%s message=%q", s.ID, status, message)
	if s.onStatus != nil {
		s.onStatus(s.ID, status, message)
	}
	if s.onClosed != nil && status != models.StatusDisconnected {
		s.onClosed(s.ID)
	}
}

// buildClientConfig 根据节点配置构造 SSH 客户端配置。
// 在鉴权步骤中通过 HostKeyCallback / BannerCallback / AuthCallback 上报详细日志。
func buildClientConfig(node models.ServerNode, report progressFn) (*ssh.ClientConfig, error) {
	auths, err := buildAuthMethods(node)
	if err != nil {
		return nil, err
	}
	if node.AuthType == "privateKey" {
		report(models.ConnectStepAuth, "running", i18n.T("鉴权配置：私钥认证"), "")
	} else if node.AuthType == "agent" || node.AuthType == "auto" {
		report(models.ConnectStepAuth, "running", i18n.T("鉴权配置：")+node.AuthType, "")
	} else {
		report(models.ConnectStepAuth, "running", i18n.T("鉴权配置：密码认证（含键盘交互）"), "")
	}
	return &ssh.ClientConfig{
		User: node.User,
		Auth: auths,
		HostKeyCallback: func(host string, remote net.Addr, key ssh.PublicKey) error {
			report(models.ConnectStepAuth, "running",
				fmt.Sprintf(i18n.T("服务端主机密钥：%s 指纹 sha256:%s"), key.Type(), ssh.FingerprintSHA256(key)), "")
			if node.HostKeyAlias != "" {
				host = net.JoinHostPort(node.HostKeyAlias, strconv.Itoa(node.Port))
			}
			return verifyConnectionHostSources(host, remote, key, node.KnownHostsSources)
		},
		BannerCallback: func(message string) error {
			report(models.ConnectStepAuth, "running", fmt.Sprintf(i18n.T("服务端横幅：%s"), message), "")
			return nil
		},
		AuthCallback: func(ctx *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			if len(ctx.AllowedMethods) > 0 {
				report(models.ConnectStepAuth, "running",
					fmt.Sprintf(i18n.T("服务器允许的鉴权方式：%s"), strings.Join(ctx.AllowedMethods, ", ")), "")
			}
			if len(ctx.TriedMethods) > 0 {
				report(models.ConnectStepAuth, "running",
					fmt.Sprintf(i18n.T("已失败的鉴权尝试：%s"), strings.Join(ctx.TriedMethods, ", ")), "")
			}
			return nil, nil
		},
		Timeout: 0,
	}, nil
}

func buildAuthMethods(node models.ServerNode) ([]ssh.AuthMethod, error) {
	switch node.AuthType {
	case "agent":
		return []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) { return AgentSigners(node) })}, nil
	case "auto":
		return []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) { return autoSigners(node) })}, nil
	case "privateKey":
		data, err := privateKeyData(node)
		if err != nil {
			return nil, err
		}
		phrase := node.RuntimePassphrases[node.KeyPath]
		if phrase == "" {
			phrase = node.Password
		}
		key, err := parsePrivateKey(data, phrase)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(key)}, nil
	case "password", "": // legacy password
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
	default:
		return nil, fmt.Errorf(i18n.T("不支持的认证方式：%s"), node.AuthType)
	}
}

// connectTimeout SSH 连接（TCP 拨号 + 握手）超时时间。
const connectTimeout = 10 * time.Second

// dialSSH 建立 SSH 连接，按 DNS / TCP / 鉴权三步上报详细日志，
// TCP 拨号与握手合计 10 秒超时，超时返回明确提示。
func dialSSH(host string, port int, config *ssh.ClientConfig, report progressFn) (*ssh.Client, error) {
	return dialSSHContext(context.Background(), host, port, config, report)
}
func dialSSHContext(ctx context.Context, host string, port int, config *ssh.ClientConfig, report progressFn) (*ssh.Client, error) {
	// 步骤 1：DNS / 直连。
	report(models.ConnectStepDNS, "running", fmt.Sprintf(i18n.T("目标主机：%s"), host), "")
	if ip := net.ParseIP(host); ip != nil {
		report(models.ConnectStepDNS, "running", fmt.Sprintf(i18n.T("主机为 IP 地址，采用直连（%s）"), ip), "")
	} else {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			msg := i18n.T("DNS 解析失败")
			report(models.ConnectStepDNS, "error", fmt.Sprintf(i18n.T("解析 %s 失败：%v"), host, err), msg)
			return nil, fmt.Errorf("%s: %w", msg, err)
		}
		addrs := make([]string, 0, len(ips))
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
		report(models.ConnectStepDNS, "running", fmt.Sprintf(i18n.T("DNS 解析成功：%s"), strings.Join(addrs, ", ")), "")
	}
	report(models.ConnectStepDNS, "done", fmt.Sprintf(i18n.T("目标地址已确定：%s:%d"), host, port), "")

	// 步骤 2：TCP 握手。
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	report(models.ConnectStepTCP, "running", fmt.Sprintf(i18n.T("正在建立 TCP 连接 %s …"), addr), "")
	dialStart := time.Now()
	conn, err := (&net.Dialer{Timeout: connectTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		cerr := connectError(err)
		report(models.ConnectStepTCP, "error", fmt.Sprintf(i18n.T("TCP 连接失败：%v"), err), cerr.Error())
		return nil, cerr
	}
	report(models.ConnectStepTCP, "done", fmt.Sprintf(i18n.T("TCP 连接成功，耗时 %dms，对端 %s"),
		time.Since(dialStart).Milliseconds(), conn.RemoteAddr().String()), "")

	stopCancel := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopCancel()
	// 步骤 3：SSH 鉴权（版本协商 + 密钥交换 + 用户鉴权）。
	// 握手阶段受 10 秒 deadline 约束，避免服务端无响应时无限等待。
	if err := conn.SetDeadline(time.Now().Add(connectTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf(i18n.T("SSH 连接失败: %w"), err)
	}
	report(models.ConnectStepAuth, "running", i18n.T("开始 SSH 版本协商与密钥交换…"), "")
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		cerr := connectError(err)
		report(models.ConnectStepAuth, "error", fmt.Sprintf(i18n.T("SSH 握手失败：%v"), err), cerr.Error())
		return nil, cerr
	}
	report(models.ConnectStepAuth, "running", fmt.Sprintf(i18n.T("服务端版本：%s"), sshConn.ServerVersion()), "")
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshConn.Close()
		return nil, fmt.Errorf(i18n.T("SSH 连接失败: %w"), err)
	}
	report(models.ConnectStepAuth, "done", fmt.Sprintf(i18n.T("SSH 鉴权成功（用户 %s）"), config.User), "")
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// connectError 将超时类错误转换为明确的超时提示。
func connectError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New(i18n.T("SSH 连接超时（10 秒）"))
	}
	return fmt.Errorf(i18n.T("SSH 连接失败: %w"), err)
}

// privateKeyData 返回私钥内容：优先使用粘贴的私钥内容，其次读取私钥文件。
func privateKeyData(node models.ServerNode) ([]byte, error) {
	if content := strings.TrimSpace(node.KeyContent); content != "" {
		return []byte(content), nil
	}
	if strings.TrimSpace(node.KeyPath) == "" {
		return nil, errors.New(i18n.T("请填写私钥内容或选择私钥文件"))
	}
	return readKey(node.KeyPath)
}

// readKey 读取私钥文件内容。
func readKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("读取私钥文件失败: %w"), err)
	}
	return data, nil
}

func parsePrivateKey(data []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		key, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf(i18n.T("解析加密私钥失败: %w"), err)
		}
		return key, nil
	}
	key, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("解析私钥失败: %w"), err)
	}
	return key, nil
}

// keepAliveLoop 定期发送心跳。服务端拒绝请求也表示 SSH 连接仍能收发。
func (s *Session) keepAliveLoop(client *ssh.Client) {
	const replyTimeout = 10 * time.Second
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		closed := s.closed || s.closing || s.client != client
		enabled := s.keepAliveEnabled
		s.mu.Unlock()
		if closed {
			return
		}
		if !enabled {
			_, _, _ = client.SendRequest("keepalive@openssh.com", false, nil)
			continue
		}

		result := make(chan error, 1)
		go func() {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			result <- err
		}()
		timer := time.NewTimer(replyTimeout)
		select {
		case err := <-result:
			timer.Stop()
			if err == nil {
				continue
			}
			logx.Warnf("SSH 心跳请求失败: session=%s err=%v", s.ID, err)
			s.closeClient(client, models.StatusDisconnected, i18n.T("连接已断开（心跳请求失败）"))
		case <-timer.C:
			logx.Warnf("SSH 心跳未在期限内收到回复: session=%s timeout=%s", s.ID, replyTimeout)
			s.closeClient(client, models.StatusDisconnected, i18n.T("连接已断开（心跳超时）"))
		}
		return
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
	if !s.closed || s.closing || s.reconnecting {
		s.mu.Unlock()
		return fmt.Errorf("%s", i18n.T("会话仍在连接中，无需重连"))
	}
	server := s.server
	onStatus := s.onStatus
	onProgress := s.onProgress
	s.reconnecting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.reconnecting = false
		s.mu.Unlock()
	}()

	report := func(step, status, log, message string) {
		if onProgress != nil {
			onProgress(s.ID, step, status, log, message)
		}
	}
	report(models.ConnectStepDNS, "running", i18n.T("正在重新连接…"), "")

	client, err := dialNode(server, report)
	if err != nil {
		return err
	}

	report(models.ConnectStepPTY, "running", i18n.T("正在创建 SSH 会话通道…"), "")
	shell, err := client.NewSession()
	if err != nil {
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf(i18n.T("创建 SSH 会话失败：%v"), err), i18n.T("创建 SSH 会话失败"))
		return fmt.Errorf(i18n.T("创建 SSH 会话失败: %w"), err)
	}

	report(models.ConnectStepPTY, "running", fmt.Sprintf(i18n.T("请求 PTY（xterm-256color %d×%d）…"), cols, rows), "")
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepPTY, "error", fmt.Sprintf(i18n.T("请求 PTY 失败：%v"), err), i18n.T("请求 PTY 失败"))
		return fmt.Errorf(i18n.T("请求 PTY 失败: %w"), err)
	}
	report(models.ConnectStepPTY, "done", i18n.T("PTY 分配成功"), "")

	report(models.ConnectStepReady, "running", i18n.T("正在获取 IO 管道…"), "")
	stdin, err := shell.StdinPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stdin 失败：%v"), err), i18n.T("获取 stdin 失败"))
		return fmt.Errorf(i18n.T("获取 stdin 失败: %w"), err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stdout 失败：%v"), err), i18n.T("获取 stdout 失败"))
		return fmt.Errorf(i18n.T("获取 stdout 失败: %w"), err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("获取 stderr 失败：%v"), err), i18n.T("获取 stderr 失败"))
		return fmt.Errorf(i18n.T("获取 stderr 失败: %w"), err)
	}

	report(models.ConnectStepReady, "running", i18n.T("正在启动 Shell…"), "")
	if err := shell.Shell(); err != nil {
		shell.Close()
		client.Close()
		report(models.ConnectStepReady, "error", fmt.Sprintf(i18n.T("启动 Shell 失败：%v"), err), i18n.T("启动 Shell 失败"))
		return fmt.Errorf(i18n.T("启动 Shell 失败: %w"), err)
	}
	report(models.ConnectStepReady, "done", i18n.T("Shell 已启动，会话就绪"), "")

	s.mu.Lock()
	s.client = client
	s.shell = shell
	s.stdin = stdin
	s.sftpState = &sessionSFTP{}
	s.closed = false
	s.keepAliveEnabled = keepAliveEnabled
	s.mu.Unlock()

	go s.keepAliveLoop(client)
	if s.onDirSync != nil && !s.server.JumpServerMode {
		s.startDirSync()
	}
	go s.pump(stdout)
	go s.pump(stderr)

	go func() {
		_ = shell.Wait()
		s.closeClient(client, models.StatusClosed, "")
	}()

	if onStatus != nil {
		onStatus(s.ID, models.StatusConnected, i18n.T("已重新连接"))
	}

	return nil
}
