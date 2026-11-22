package sshx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ExecRequest runs on a separate, non-PTY channel of the exact current transport.
// Raw mode preserves the server's command semantics. cwd/env require POSIX mode.
type ExecRequest struct {
	SessionID string            `json:"sessionId"`
	Command   string            `json:"command"`
	Mode      string            `json:"mode,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type ExecResult struct {
	ExitCode          *int   `json:"exitCode,omitempty"`
	Signal            string `json:"signal,omitempty"`
	Known             bool   `json:"known"`
	Cancelled         bool   `json:"cancelled"`
	CancelRequested   bool   `json:"cancelRequested"`
	TerminationProven bool   `json:"terminationProven"`
	Error             string `json:"error,omitempty"`
}

type PreparedExec struct {
	client  *ssh.Client
	command string
}

// x/crypto/ssh cannot cancel NewSession or a pending exec request without
// closing the entire transport. Keep a slot until the underlying operation
// actually ends, even when the caller already timed out, so an unresponsive
// peer cannot accumulate an unbounded number of abandoned goroutines.
type execTransportBudget struct{ slots chan struct{} }

var execTransportBudgets sync.Map

func transportExecSlots(client *ssh.Client) chan struct{} {
	created := &execTransportBudget{slots: make(chan struct{}, 8)}
	v, loaded := execTransportBudgets.LoadOrStore(client, created)
	budget := v.(*execTransportBudget)
	if !loaded {
		go func() {
			_ = client.Wait()
			execTransportBudgets.CompareAndDelete(client, budget)
		}()
	}
	return budget.slots
}

var execEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ExecCommand validates arguments and never silently changes raw shell syntax.
func ExecCommand(req ExecRequest) (string, error) {
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > 256<<10 || strings.IndexByte(req.Command, 0) >= 0 {
		return "", errors.New("command 必须是非空文本，最多 256 KiB 且不能包含 NUL")
	}
	if req.Mode == "" || req.Mode == "raw" {
		if req.Cwd != "" || len(req.Env) != 0 {
			return "", errors.New("cwd/env 只支持显式 mode=posix；raw 模式不改写命令")
		}
		return req.Command, nil
	}
	if req.Mode != "posix" {
		return "", errors.New("mode 必须是 raw 或 posix")
	}
	if strings.IndexByte(req.Cwd, 0) >= 0 || len(req.Cwd) > 8192 || len(req.Env) > 128 {
		return "", errors.New("cwd/env 超出限制或包含 NUL")
	}
	var script strings.Builder
	if req.Cwd != "" {
		script.WriteString("cd -- " + shellSingleQuote(req.Cwd) + " && ")
	}
	keys := make([]string, 0, len(req.Env))
	for key, value := range req.Env {
		if !execEnvName.MatchString(key) || strings.IndexByte(value, 0) >= 0 || len(value) > 32768 {
			return "", errors.New("env 包含无效变量名或值")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		script.WriteString("env ")
		for _, key := range keys {
			script.WriteString(shellSingleQuote(key+"="+req.Env[key]) + " ")
		}
	}
	script.WriteString("sh -c " + shellSingleQuote(req.Command))
	return "sh -c " + shellSingleQuote(script.String()), nil
}

// PrepareExec pins the transport so reconnecting a session cannot retarget a job.
func (m *Manager) PrepareExec(req ExecRequest) (*PreparedExec, error) {
	command, err := ExecCommand(req)
	if err != nil {
		return nil, err
	}
	s, err := m.get(req.SessionID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.closed || s.closing || s.reconnecting {
		return nil, errors.New("SSH 会话未连接或正在重连")
	}
	if s.server.JumpServerMode {
		return nil, errors.New("JumpServer 交互网关不支持独立 exec；请使用交互终端")
	}
	return &PreparedExec{client: s.client, command: command}, nil
}

type execOutput struct {
	stream string
	emit   func(string, []byte)
	mu     *sync.Mutex
}

func (w execOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emit != nil {
		w.emit(w.stream, p)
	}
	return len(p), nil
}

func execWaitResult(err error) ExecResult {
	if err == nil {
		n := 0
		return ExecResult{ExitCode: &n, Known: true, TerminationProven: true}
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		n := exit.ExitStatus()
		return ExecResult{ExitCode: &n, Signal: exit.Signal(), Known: true, TerminationProven: true}
	}
	return ExecResult{Known: false, Error: fmt.Sprintf("SSH exec 结果未知：%v", err)}
}

// Run closes only this job's channel on cancellation. Closing an SSH channel does
// not prove the remote process stopped, so that path explicitly returns unknown.
func (p *PreparedExec) Run(ctx context.Context, emit func(string, []byte)) ExecResult {
	if err := ctx.Err(); err != nil {
		return ExecResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: err.Error()}
	}
	slots := transportExecSlots(p.client)
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ExecResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: "SSH 独立通道预算等待已取消，命令未执行"}
	}
	release := func() { <-slots }
	type opened struct {
		s   *ssh.Session
		err error
	}
	ready := make(chan opened)
	go func() {
		s, e := p.client.NewSession()
		select {
		case ready <- opened{s, e}:
		case <-ctx.Done():
			if s != nil {
				_ = s.Close()
			}
			release()
		}
	}()
	var session *ssh.Session
	select {
	case item := <-ready:
		if item.err != nil {
			release()
			return execWaitResult(item.err)
		}
		session = item.s
	case <-ctx.Done():
		return ExecResult{Known: false, CancelRequested: true, Error: "打开 SSH exec 通道已取消，未启动命令"}
	}
	defer session.Close()
	if err := ctx.Err(); err != nil {
		release()
		return ExecResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: "SSH exec 启动前已取消，命令未执行"}
	}
	var writeMu sync.Mutex
	session.Stdout = execOutput{"stdout", emit, &writeMu}
	session.Stderr = execOutput{"stderr", emit, &writeMu}
	session.Stdin = strings.NewReader("")
	done := make(chan error, 1)
	go func() {
		defer release()
		done <- session.Run(p.command)
	}()
	select {
	case err := <-done:
		return execWaitResult(err)
	case <-ctx.Done():
		// Prefer a server exit acknowledgement; a successful signal request alone
		// does not establish that the process or its descendants terminated.
		go func() { _ = session.Signal(ssh.SIGTERM) }()
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case err := <-done:
			r := execWaitResult(err)
			r.CancelRequested = true
			if r.Known && r.Signal != "" {
				r.Cancelled = true
			}
			return r
		case <-timer.C:
		}
		_ = session.Close()
		return ExecResult{Known: false, CancelRequested: true, TerminationProven: false, Error: "已关闭任务通道；未确认远端进程或子进程已终止"}
	}
}

var _ io.Writer = execOutput{}
