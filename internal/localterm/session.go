package localterm

import (
	"ding-ssh/internal/i18n"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	gopty "github.com/aymanbagabas/go-pty"
)

// Session 本机交互式终端会话。
type Session struct {
	ID        string
	shell     string
	label     string
	createdAt int64

	pty gopty.Pty
	cmd *gopty.Cmd

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once

	onOutput func(id string, data []byte)
	onStatus func(id string, status models.SessionStatus, message string)
	onClosed func(id string)
}

// 本地终端输出是逐块字节流：采样记录块数与累计字节，绝不记录终端内容。
var localOutputLog = logx.NewSampler(64, 2*time.Second)

// 终端输入同样逐键触发：采样记录字节数。
var localInputLog = logx.NewSampler(128, 2*time.Second)

func newSession(
	id string,
	shellPref string,
	cols, rows int,
	onOutput func(string, []byte),
	onStatus func(string, models.SessionStatus, string),
	onClosed func(string),
) (*Session, error) {
	path, args, label, err := Resolve(shellPref)
	if err != nil {
		logx.Warnf("[localterm] 解析本地 Shell 失败: session=%s shell=%s err=%v", id, shellPref, err)
		return nil, err
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	logx.Debugf("[localterm] 准备创建本地终端: session=%s shell=%s path=%q args=%d size=%dx%d",
		id, label, path, len(args), cols, rows)

	pt, err := gopty.New()
	if err != nil {
		logx.Warnf("[localterm] 创建本地 PTY 失败: session=%s err=%v", id, err)
		return nil, fmt.Errorf(i18n.T("创建本地 PTY 失败: %w"), err)
	}
	if err := pt.Resize(cols, rows); err != nil {
		_ = pt.Close()
		logx.Warnf("[localterm] 设置本地终端初始尺寸失败: session=%s size=%dx%d err=%v", id, cols, rows, err)
		return nil, fmt.Errorf(i18n.T("设置本地终端尺寸失败: %w"), err)
	}

	cmd := pt.Command(path, args...)
	cmd.Env = augmentEnv(os.Environ())
	if home, herr := os.UserHomeDir(); herr == nil {
		cmd.Dir = home
	} else {
		logx.Warnf("[localterm] 获取用户主目录失败，沿用当前工作目录: session=%s err=%v", id, herr)
	}

	s := &Session{
		ID:        id,
		shell:     Normalize(shellPref),
		label:     label,
		createdAt: time.Now().UnixMilli(),
		pty:       pt,
		cmd:       cmd,
		onOutput:  onOutput,
		onStatus:  onStatus,
		onClosed:  onClosed,
	}

	if err := cmd.Start(); err != nil {
		_ = pt.Close()
		logx.Warnf("[localterm] 启动本地 Shell 失败: session=%s label=%s path=%q err=%v", id, label, path, err)
		return nil, fmt.Errorf(i18n.T("启动本地 Shell（%s）失败: %w"), label, err)
	}
	logx.Debugf("[localterm] 本地终端已启动: session=%s shell=%s path=%q", id, label, path)

	go s.pump()
	go func() {
		_ = cmd.Wait()
		s.close(models.StatusClosed, "")
	}()

	return s, nil
}

func augmentEnv(base []string) []string {
	out := make([]string, 0, len(base)+2)
	hasTerm := false
	hasColor := false
	for _, e := range base {
		if strings.HasPrefix(e, "TERM=") {
			hasTerm = true
		}
		if strings.HasPrefix(e, "COLORTERM=") {
			hasColor = true
		}
		out = append(out, e)
	}
	if !hasTerm {
		out = append(out, "TERM=xterm-256color")
		logx.Debugf("[localterm] 环境变量缺少 TERM，已补默认值: TERM=xterm-256color")
	}
	if !hasColor {
		out = append(out, "COLORTERM=truecolor")
		logx.Debugf("[localterm] 环境变量缺少 COLORTERM，已补默认值: COLORTERM=truecolor")
	}
	return out
}

func (s *Session) pump() {
	start := time.Now()
	buf := make([]byte, 32*1024)
	var total int64
	var chunks int64
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			total += int64(n)
			chunks++
			// 逐块字节流：只采样记录块数与累计字节，绝不记录终端内容。
			logx.DebugSampled(localOutputLog, "[localterm] 本地终端输出: session=%s bytes=%d total=%d",
				s.ID, n, total)
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if s.onOutput != nil {
				s.onOutput(s.ID, chunk)
			}
		}
		if err != nil {
			if err != io.EOF {
				logx.Warnf("[localterm] 本地终端读取异常: session=%s totalBytes=%d chunks=%d err=%v",
					s.ID, total, chunks, err)
			} else {
				logx.Debugf("[localterm] 本地终端读取结束(EOF): session=%s totalBytes=%d chunks=%d elapsed=%s",
					s.ID, total, chunks, time.Since(start))
			}
			s.close(models.StatusClosed, "")
			return
		}
	}
}

// Write 向本地终端写入用户输入。
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		logx.WarnSampled(localInputLog, "[localterm] 写入本地终端被拒绝: session=%s 已关闭", s.ID)
		return fmt.Errorf("%s", i18n.T("本地会话已关闭"))
	}
	if _, err := s.pty.Write(data); err != nil {
		logx.WarnSampled(localInputLog, "[localterm] 本地终端写入失败: session=%s bytes=%d err=%v", s.ID, len(data), err)
		return err
	}
	// 逐键输入最高频：采样记录字节数，绝不记录输入内容。
	logx.DebugSampled(localInputLog, "[localterm] 本地终端写入: session=%s bytes=%d", s.ID, len(data))
	return nil
}

// Resize 调整本地终端尺寸。
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("%s", i18n.T("本地会话已关闭"))
	}
	if cols <= 0 || rows <= 0 {
		logx.Debugf("[localterm] 忽略非法终端尺寸: session=%s size=%dx%d", s.ID, cols, rows)
		return nil
	}
	if err := s.pty.Resize(cols, rows); err != nil {
		logx.Warnf("[localterm] 本地终端尺寸调整失败: session=%s size=%dx%d err=%v", s.ID, cols, rows, err)
		return err
	}
	logx.Debugf("[localterm] 本地终端尺寸已调整: session=%s size=%dx%d", s.ID, cols, rows)
	return nil
}

func (s *Session) close(status models.SessionStatus, message string) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		if s.pty != nil {
			_ = s.pty.Close()
		}
		if s.onStatus != nil {
			s.onStatus(s.ID, status, message)
		}
		if s.onClosed != nil {
			s.onClosed(s.ID)
		}
		logx.Debugf("[localterm] 本地终端已关闭: session=%s status=%s reason=%q elapsed=%s",
			s.ID, status, message, time.Since(time.UnixMilli(s.createdAt)))
	})
}

// Info 返回会话摘要。
func (s *Session) Info() models.SessionInfo {
	logx.Debugf("[localterm] 读取本地终端会话摘要: session=%s shell=%s", s.ID, s.label)
	return models.SessionInfo{
		SessionID:  s.ID,
		ServerName: i18n.T("本机 · ") + s.label,
		Host:       "localhost",
		User:       s.label,
		Status:     models.StatusConnected,
		CreatedAt:  s.createdAt,
	}
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
