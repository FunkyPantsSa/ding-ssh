package sshx

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/models"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const sudoPromptLifetime = 30 * time.Second

var (
	sudoCommand = regexp.MustCompile(`^(?:sudo|/usr/bin/sudo|/bin/sudo)[ \t]+-(?:i|s)[ \t]*$`)
	sudoPrompt  = regexp.MustCompile(`(?i)^\[sudo\][ \t]+(?:password for ([^:\r\n]+):|([^:\r\n：]+?)[ \t]*的密码[：:])[ \t]*$`)
	sudoColor   = regexp.MustCompile("\x1b\\[[0-9;]*m")
	sudoOSC     = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	// A nested interactive destination cannot be mapped back to the saved server.
	// Conservatively disable the assistant until this SSH session is reconnected.
	sudoNested = regexp.MustCompile(`(?:^|[;&| \t])(?:ssh|mosh|su|telnet|tmux|screen|docker|podman|kubectl|exec)(?:[ \t]|$)`)
)

// All fields are protected by Session.mu. No password is kept in this state.
type sudoAssistantState struct {
	until     time.Time
	requestID string
	output    string
	user      string
	submitted bool
	muted     bool
	disabled  bool
}

func sudoExpired() error {
	return errors.New(i18n.T("sudo 密码提示已失效，请在终端重新操作"))
}

func (s *Session) sudoEventLocked(state string, sensitive bool) models.SudoPromptEvent {
	s.sudoSequence++
	return models.SudoPromptEvent{
		Sequence:  s.sudoSequence,
		RequestID: s.sudo.requestID, State: state, User: s.sudo.user,
		HasSavedPassword: !s.sudo.submitted && s.sudo.user == s.server.User &&
			(s.server.AuthType == "password" || s.server.AuthType == "") && s.server.Password != "",
		Retry: s.sudo.submitted, Sensitive: sensitive, ExpiresAt: s.sudo.until.UnixMilli(),
	}
}

// notifySudo is deliberately called without the session state lock held.
func (s *Session) notifySudo(event *models.SudoPromptEvent) {
	if event == nil {
		return
	}
	s.mu.Lock()
	fn := s.onSudoPrompt
	s.mu.Unlock()
	if fn != nil {
		fn(*event)
	}
}

// observeSudoOutput runs before the ordinary output event, so the UI can turn off
// completion/history before any password keystrokes arrive. Only a complete,
// current, unterminated prompt may hold a valid one-use request ID.
func (s *Session) observeSudoOutput(data []byte) {
	s.mu.Lock()
	st := &s.sudo
	if st.until.IsZero() {
		s.mu.Unlock()
		return
	}
	var event *models.SudoPromptEvent
	if s.closed || s.closing || time.Now().After(st.until) {
		disabled := st.disabled
		*st = sudoAssistantState{disabled: disabled}
		e := s.sudoEventLocked("close", false)
		event = &e
	} else {
		st.output += string(data)
		if len(st.output) > 4096 {
			st.output = st.output[len(st.output)-4096:]
		}
		visible := sudoColor.ReplaceAllString(sudoOSC.ReplaceAllString(st.output, ""), "")
		last := visible[strings.LastIndexAny(visible, "\r\n")+1:]
		match := sudoPrompt.FindStringSubmatch(last)
		if len(match) > 0 && !strings.ContainsAny(last, "\x1b\b\x7f") {
			if st.requestID == "" {
				st.user = strings.TrimSpace(match[1] + match[2])
				st.requestID = uuid.NewString()
				state := "prompt"
				if st.muted {
					state = "input"
				}
				e := s.sudoEventLocked(state, true)
				event = &e
			}
		} else if st.requestID != "" {
			st.requestID = ""
			e := s.sudoEventLocked("close", false)
			event = &e
		}
	}
	s.mu.Unlock()
	s.notifySudo(event)
}

// writeInteractive arms only a simple sudo login/shell command submitted with a
// single Enter. The command is a hint read from the rendered input line; the
// actual password prompt must still be independently observed on the backend.
func (s *Session) writeInteractive(data []byte, command string) error {
	return s.writeTerminal(data, command, true)
}

func (s *Session) writeTerminal(data []byte, command string, interactive bool) error {
	s.writeMu.Lock()
	s.mu.Lock()
	if s.closed || s.closing || s.reconnecting || s.stdin == nil {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return errors.New(i18n.T("会话已关闭"))
	}
	var event *models.SudoPromptEvent
	st := &s.sudo
	hadPrompt := st.requestID != ""
	hadOperation := !st.until.IsZero()
	enter := string(data) == "\r" || string(data) == "\n"
	if interactive && enter && command != "" && sudoNested.MatchString(command) {
		st.disabled = true
	}
	if interactive && !enter && strings.ContainsAny(string(data), "\r\n") {
		// Multiline pastes can change destination before the final visible line.
		st.disabled = true
	}
	if interactive && enter && sudoCommand.MatchString(strings.TrimLeft(command, " \t")) &&
		s.server.SudoAutoFill && !s.server.JumpServerMode && !st.disabled {
		*st = sudoAssistantState{until: time.Now().Add(sudoPromptLifetime)}
	} else if len(data) > 0 {
		// Any user/automation input consumes the old challenge before writing.
		st.requestID = ""
		st.output = ""
		if !interactive || command != "" || strings.ContainsAny(string(data), "\x03\x04") || time.Now().After(st.until) {
			disabled := st.disabled
			*st = sudoAssistantState{disabled: disabled}
		} else if enter {
			st.submitted = true
		}
	}
	if hadPrompt || (hadOperation && (enter || strings.ContainsAny(string(data), "\x03\x04"))) {
		state, sensitive := "close", false
		if !st.until.IsZero() && !enter {
			state, sensitive = "input", true
		}
		e := s.sudoEventLocked(state, sensitive)
		event = &e
	}
	stdin := s.stdin
	s.mu.Unlock()
	s.notifySudo(event)
	n, err := stdin.Write(data)
	s.writeMu.Unlock()
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (s *Session) sudoServer(requestID string) (models.ServerNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if requestID == "" || requestID != s.sudo.requestID || s.closed || s.closing || s.reconnecting || time.Now().After(s.sudo.until) {
		return models.ServerNode{}, sudoExpired()
	}
	return s.server, nil
}

// submitSudoPassword revalidates after credential lookup, then atomically
// consumes the challenge and writes one password plus Enter. No shell command,
// remote temporary file, clipboard or frontend command-history path is used.
func (s *Session) submitSudoPassword(requestID, password string, saved bool) error {
	if password == "" || strings.IndexFunc(password, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return errors.New(i18n.T("请输入有效密码，密码不能包含终端控制字符"))
	}
	s.writeMu.Lock()
	s.mu.Lock()
	st := &s.sudo
	if requestID == "" || requestID != st.requestID || s.closed || s.closing || s.reconnecting || time.Now().After(st.until) ||
		(saved && (st.submitted || st.user != s.server.User || (s.server.AuthType != "password" && s.server.AuthType != ""))) {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return sudoExpired()
	}
	st.requestID, st.output, st.submitted = "", "", true
	stdin := s.stdin
	s.mu.Unlock()
	data := []byte(password + "\r")
	n, err := stdin.Write(data)
	clear(data)
	s.writeMu.Unlock()
	if err == nil && n != len(password)+1 {
		err = io.ErrShortWrite
	}
	// Underlying writers must not be allowed to include secret data in errors.
	if err != nil {
		return errors.New(i18n.T("sudo 密码提交失败，请检查会话连接"))
	}
	return nil
}

func (s *Session) cancelSudoPrompt(requestID string) {
	s.mu.Lock()
	if requestID == "" || requestID != s.sudo.requestID {
		s.mu.Unlock()
		return
	}
	s.sudo.muted = true
	s.sudo.requestID = ""
	event := s.sudoEventLocked("input", true)
	s.mu.Unlock()
	s.notifySudo(&event)
}

func (m *Manager) WriteInteractive(sessionID, dataBase64, command string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	data, err := decodeBase64(dataBase64)
	if err != nil {
		return err
	}
	return s.writeInteractive(data, command)
}

func (m *Manager) WriteRawInteractive(sessionID string, data []byte, command string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.writeInteractive(data, command)
}

func (m *Manager) SudoServer(sessionID, requestID string) (models.ServerNode, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return models.ServerNode{}, err
	}
	return s.sudoServer(requestID)
}

func (m *Manager) SubmitSudoPassword(sessionID, requestID, password string, saved bool) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.submitSudoPassword(requestID, password, saved)
}

func (m *Manager) CancelSudoPrompt(sessionID, requestID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	s.cancelSudoPrompt(requestID)
	return nil
}
