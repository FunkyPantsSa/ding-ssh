package sshx

import (
	"bytes"
	"ding-ssh/internal/models"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type sudoTestInput struct {
	mu   sync.Mutex
	data bytes.Buffer
	fail bool
}

func (w *sudoTestInput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("writer leaked: " + string(data))
	}
	return w.data.Write(data)
}
func (*sudoTestInput) Close() error   { return nil }
func (w *sudoTestInput) text() string { w.mu.Lock(); defer w.mu.Unlock(); return w.data.String() }

func sudoFixture() (*Session, *sudoTestInput, *[]models.SudoPromptEvent) {
	input := &sudoTestInput{}
	events := []models.SudoPromptEvent{}
	s := &Session{ID: "one", stdin: input, server: models.ServerNode{
		ID: "server", Host: "test.invalid", Port: 22, User: "operator", AuthType: "password", Password: "stored-secret",
		SSHOptions: models.SSHOptions{SudoAutoFill: true},
	}}
	s.onSudoPrompt = func(event models.SudoPromptEvent) { events = append(events, event) }
	return s, input, &events
}
func armSudo(t *testing.T, s *Session) string {
	t.Helper()
	if err := s.writeInteractive([]byte("\r"), "sudo -i"); err != nil {
		t.Fatal(err)
	}
	s.observeSudoOutput([]byte("\r\n[sudo] password for operator: "))
	if s.sudo.requestID == "" {
		t.Fatal("missing challenge")
	}
	return s.sudo.requestID
}

func TestSudoAssistantRequiresSubmittedCommandAndEnabledServer(t *testing.T) {
	for _, name := range []string{"not-armed", "disabled", "jump-server", "complex-command", "automation"} {
		t.Run(name, func(t *testing.T) {
			s, _, events := sudoFixture()
			switch name {
			case "disabled":
				s.server.SudoAutoFill = false
				_ = s.writeInteractive([]byte("\r"), "sudo -i")
			case "jump-server":
				s.server.JumpServerMode = true
				_ = s.writeInteractive([]byte("\r"), "sudo -i")
			case "complex-command":
				_ = s.writeInteractive([]byte("\r"), "echo hello; sudo -i")
			case "automation":
				_ = s.Write([]byte("sudo -i\r"))
			}
			s.observeSudoOutput([]byte("[sudo] password for operator: "))
			if len(*events) != 0 || s.sudo.requestID != "" {
				t.Fatal("unexpected password challenge")
			}
		})
	}
}

func TestSudoAssistantMatchesSplitColoredAndChinesePrompts(t *testing.T) {
	for _, prompt := range []string{"[sudo] password for operator: ", "\x1b[33m[sudo] password for operator:\x1b[0m ", "[sudo] operator 的密码："} {
		s, _, events := sudoFixture()
		_ = s.writeInteractive([]byte("\r"), "sudo -s")
		for _, b := range []byte("\r\n" + prompt) {
			s.observeSudoOutput([]byte{b})
		}
		last := (*events)[len(*events)-1]
		if last.State != "prompt" || !last.HasSavedPassword || last.User != "operator" {
			t.Fatalf("unexpected events: %+v", *events)
		}
		// ANSI resets may be split immediately after an already complete prompt.
		if s.sudo.requestID == "" {
			t.Fatal("colored prompt lost its challenge")
		}
	}
}

func TestSudoAssistantDoesNotUseCompletedHistoricalPrompts(t *testing.T) {
	s, _, events := sudoFixture()
	_ = s.writeInteractive([]byte("\r"), "sudo -i")
	s.observeSudoOutput([]byte("[sudo] password for operator: \r\noperator@test:~$ "))
	if len(*events) != 0 {
		t.Fatal("historical prompt triggered helper")
	}
}

func TestSudoAssistantPasswordOnlyOnceAndManualRetry(t *testing.T) {
	s, input, events := sudoFixture()
	id := armSudo(t, s)
	if err := s.submitSudoPassword(id, "stored-secret", true); err != nil {
		t.Fatal(err)
	}
	if input.text() != "\rstored-secret\r" {
		t.Fatal("password not sent directly with one Enter")
	}
	if err := s.submitSudoPassword(id, "stored-secret", true); err == nil {
		t.Fatal("replayed challenge")
	}
	s.observeSudoOutput([]byte("\r\nSorry, try again.\r\n[sudo] password for operator: "))
	retry := (*events)[len(*events)-1]
	if !retry.Retry || retry.HasSavedPassword || retry.RequestID == id {
		t.Fatalf("invalid retry: %+v", retry)
	}
	if err := s.submitSudoPassword(retry.RequestID, "stored-secret", true); err == nil {
		t.Fatal("saved password retried")
	}
	if err := s.submitSudoPassword(retry.RequestID, "different-secret", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(input.text(), "different-secret\r") {
		t.Fatal("manual override not sent")
	}
}

func TestSudoAssistantRejectsInvalidatedTokens(t *testing.T) {
	for _, name := range []string{"cancel", "typing", "interrupt", "new-output", "expired", "closed", "reconnecting"} {
		t.Run(name, func(t *testing.T) {
			s, input, _ := sudoFixture()
			id := armSudo(t, s)
			switch name {
			case "cancel":
				s.cancelSudoPrompt(id)
			case "typing":
				_ = s.writeInteractive([]byte("x"), "")
			case "interrupt":
				_ = s.writeInteractive([]byte{3}, "")
			case "new-output":
				s.observeSudoOutput([]byte("\r\noperator@test:~$ "))
			case "expired":
				s.sudo.until = time.Now().Add(-time.Second)
			case "closed":
				s.closed = true
			case "reconnecting":
				s.reconnecting = true
			}
			if err := s.submitSudoPassword(id, "must-not-send", true); err == nil {
				t.Fatal("stale prompt accepted")
			}
			if strings.Contains(input.text(), "must-not-send") {
				t.Fatal("password sent after invalidation")
			}
		})
	}
}

func TestSudoAssistantNeverUsesKeyPhraseOrOtherAccountPassword(t *testing.T) {
	for _, name := range []string{"privateKey", "agent", "other-user"} {
		s, _, events := sudoFixture()
		if name == "other-user" {
			s.server.User = "someone-else"
		} else {
			s.server.AuthType = name
		}
		id := armSudo(t, s)
		if (*events)[0].HasSavedPassword {
			t.Fatal("offered wrong credential")
		}
		if err := s.submitSudoPassword(id, "wrong-secret", true); err == nil {
			t.Fatal("accepted wrong credential")
		}
		if err := s.submitSudoPassword(id, "manual-password", false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSudoAssistantSessionIsolationAndConcurrentSubmit(t *testing.T) {
	one, input, _ := sudoFixture()
	two, other, _ := sudoFixture()
	one.onSudoPrompt, two.onSudoPrompt = nil, nil
	id := armSudo(t, one)
	armSudo(t, two)
	m := &Manager{sessions: map[string]*Session{"one": one, "two": two}}
	if err := m.SubmitSudoPassword("two", id, "wrong-session", true); err == nil {
		t.Fatal("cross-session challenge")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = m.SubmitSudoPassword("one", id, "one-secret", true) }()
	}
	wg.Wait()
	if strings.Count(input.text(), "one-secret") != 1 || strings.Contains(other.text(), "secret") {
		t.Fatal("duplicate or cross-session secret")
	}
}

func TestSudoAssistantDisablesForNestedDestinationsAndMultilinePaste(t *testing.T) {
	for _, command := range []string{"ssh another-host", "su -", "tmux attach", "docker exec -it box bash", "kubectl exec -it pod -- sh", "exec ssh another-host"} {
		s, _, events := sudoFixture()
		_ = s.writeInteractive([]byte("\r"), command)
		_ = s.writeInteractive([]byte("\r"), "sudo -i")
		s.observeSudoOutput([]byte("[sudo] password for operator: "))
		if len(*events) != 0 {
			t.Fatalf("enabled after %s", command)
		}
	}
	s, _, events := sudoFixture()
	_ = s.writeInteractive([]byte("ssh another-host\rwhoami\r"), "")
	_ = s.writeInteractive([]byte("\r"), "sudo -i")
	s.observeSudoOutput([]byte("[sudo] password for operator: "))
	if len(*events) != 0 {
		t.Fatal("enabled after multiline paste")
	}
}

func TestSudoAssistantControlCharactersAndWriteErrorsDoNotExposeSecrets(t *testing.T) {
	s, input, _ := sudoFixture()
	id := armSudo(t, s)
	for _, password := range []string{"", "secret\ncommand", "secret\r", "secret\x15", "secret\b", "secret\x1b"} {
		if err := s.submitSudoPassword(id, password, false); err == nil {
			t.Fatal("accepted control character")
		}
	}
	input.fail = true
	err := s.submitSudoPassword(id, "must-stay-secret", false)
	if err == nil || strings.Contains(err.Error(), "must-stay-secret") {
		t.Fatal("missing error or leaked password")
	}
}
