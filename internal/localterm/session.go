package localterm

import (
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
		return nil, err
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	pt, err := gopty.New()
	if err != nil {
		return nil, fmt.Errorf("创建本地 PTY 失败: %w", err)
	}
	if err := pt.Resize(cols, rows); err != nil {
		_ = pt.Close()
		return nil, fmt.Errorf("设置本地终端尺寸失败: %w", err)
	}

	cmd := pt.Command(path, args...)
	cmd.Env = augmentEnv(os.Environ())
	if home, herr := os.UserHomeDir(); herr == nil {
		cmd.Dir = home
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
		return nil, fmt.Errorf("启动本地 Shell（%s）失败: %w", label, err)
	}
	logx.Debugf("本地终端已启动: session=%s shell=%s path=%s", id, label, path)

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
	}
	if !hasColor {
		out = append(out, "COLORTERM=truecolor")
	}
	return out
}

func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if s.onOutput != nil {
				s.onOutput(s.ID, chunk)
			}
		}
		if err != nil {
			if err != io.EOF {
				logx.Debugf("本地终端读取结束: session=%s err=%v", s.ID, err)
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
		return fmt.Errorf("本地会话已关闭")
	}
	_, err := s.pty.Write(data)
	return err
}

// Resize 调整本地终端尺寸。
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("本地会话已关闭")
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return s.pty.Resize(cols, rows)
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
		logx.Debugf("本地终端已关闭: session=%s status=%s", s.ID, status)
	})
}

// Info 返回会话摘要。
func (s *Session) Info() models.SessionInfo {
	return models.SessionInfo{
		SessionID:  s.ID,
		ServerName: "本机 · " + s.label,
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
