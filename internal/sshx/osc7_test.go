package sshx

import "testing"

func TestParsePromptUbuntuColored(t *testing.T) {
	raw := "\x1b]0;root@host: /data\x07root@host:\x1b[01;34m/data\x1b[00m# "
	p, ok := ParsePrompt([]byte(raw))
	if !ok || p != "/data" {
		t.Fatalf("got %q ok=%v", p, ok)
	}
}

func TestParsePromptCentOSBracket(t *testing.T) {
	raw := "[root@localhost /data]# "
	p, ok := ParsePrompt([]byte(raw))
	if !ok || p != "/data" {
		t.Fatalf("got %q ok=%v", p, ok)
	}
}

func TestParsePromptAfterCdEcho(t *testing.T) {
	raw := "cd /data\n[root@host /data]# "
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/data" {
		t.Fatalf("got %q src=%s ok=%v", p, src, ok)
	}
	if src != "prompt" {
		t.Fatalf("want prompt, got %s", src)
	}
}

func TestParseCdFallbackWhenPromptUnknown(t *testing.T) {
	raw := "cd /data\n❯ "
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/data" || src != "cd" {
		t.Fatalf("got %q src=%s ok=%v", p, src, ok)
	}
}

func TestParseOSC7LastMatchWins(t *testing.T) {
	raw := "\x1b]7;file://host/root\x07ls\n\x1b]7;file://host/data\x07"
	p, ok := ParseOSC7([]byte(raw))
	if !ok || p != "/data" {
		t.Fatalf("got %q ok=%v, want last OSC7 /data", p, ok)
	}
}

func TestParseOSC7EmptyHost(t *testing.T) {
	raw := "\x1b]7;file:///var/log\x07"
	p, ok := ParseOSC7([]byte(raw))
	if !ok || p != "/var/log" {
		t.Fatalf("got %q ok=%v", p, ok)
	}
}

func TestStaleOSC7DoesNotHidePrompt(t *testing.T) {
	raw := "\x1b]7;file://host/root\x07\ncd /data\nroot@host:/data# "
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/data" || src != "prompt" {
		t.Fatalf("got %q src=%s ok=%v", p, src, ok)
	}
}

func TestParsePromptTildeHome(t *testing.T) {
	raw := "root@sentry-test-141:~# "
	p, ok := ParsePrompt([]byte(raw))
	if !ok || p != "~" {
		t.Fatalf("got %q ok=%v", p, ok)
	}
}

func TestParsePromptTildeWithBEL(t *testing.T) {
	raw := "root@sentry-test-141:~# \a\nroot@sentry-test-141:~#"
	p, ok := ParsePrompt([]byte(raw))
	if !ok || p != "~" {
		t.Fatalf("got %q ok=%v", p, ok)
	}
}

func TestParsePromptAfterBackspaceEdit(t *testing.T) {
	raw := "cd /tmp\b\b\bdata\nroot@host:/data# "
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/data" || src != "prompt" {
		t.Fatalf("got %q src=%s ok=%v", p, src, ok)
	}
}
