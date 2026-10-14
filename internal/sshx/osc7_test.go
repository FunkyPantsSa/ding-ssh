package sshx

import (
	"testing"

	"ding-ssh/internal/models"
)

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

// 真实故障现场：bash 把键入的 cd 回显在提示符同一行（…$ cd /tmp），
// 旧的行首锚点匹配不到；修复后应从提示符同行回显中解析出绝对路径。
func TestParseCdEchoOnPromptLine(t *testing.T) {
	raw := "\x1b]3008;start=x;type=shell;cwd=/home/mqj/WeKnora\x1b\\[mqj@archlinux WeKnora]$ cd /tmp\r\n\x1b[?2004l\r"
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/tmp" || src != "cd" {
		t.Fatalf("got %q src=%s ok=%v, want /tmp src=cd", p, src, ok)
	}
}

// 提示符同行回显带引号：cd '/home/mqj/WeKnora'
func TestParseCdEchoQuoted(t *testing.T) {
	raw := "[mqj@archlinux kvmem-llama.cpp]$ cd '/home/mqj/WeKnora'\r\n"
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != "/home/mqj/WeKnora" || src != "cd" {
		t.Fatalf("got %q src=%s ok=%v", p, src, ok)
	}
}

// 提示符同行回显的相对参数（cd ..、cd subdir）原样返回，由 Session.expandPath
// 基于 lastDir 补全为绝对路径。
func TestParseCdEchoRelative(t *testing.T) {
	raw := "[mqj@archlinux WeKnora]$ cd ..\r\n"
	p, src, ok := ParseDirFromOutput([]byte(raw))
	if !ok || p != ".." || src != "cd" {
		t.Fatalf("got %q src=%s ok=%v, want raw '..' src=cd", p, src, ok)
	}
}

// 提示符同行回显 cd -：回显 OLDPWD 语义的 - 是特殊参数，expandPath 应拒绝。
func TestExpandPathRelativeJoinsLastDir(t *testing.T) {
	s := &Session{
		server:  models.ServerNode{User: "mqj"},
		home:    "/home/mqj",
		lastDir: "/home/mqj/WeKnora",
	}
	cases := []struct {
		in   string
		want string
	}{
		{"..", "/home/mqj"},
		{"../x", "/home/mqj/x"},
		{"sub", "/home/mqj/WeKnora/sub"},
		{"./sub", "/home/mqj/WeKnora/sub"},
		{".", ""},
		{"-", ""},
		{"/tmp", "/tmp"},
		{"~", "/home/mqj"},
		{"~/x", "/home/mqj/x"},
		{"", ""},
	}
	for _, c := range cases {
		if got := s.expandPath(c.in); got != c.want {
			t.Errorf("expandPath(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// OSC 3008 shell integration：只认 type=shell 的 cwd（命令结束后当前目录），
// 忽略 type=command 事件里命令开始前的旧目录。
func TestParseOSC3008ShellOnly(t *testing.T) {
	raw := "\x1b]3008;start=ea;type=command;machineid=m;user=mqj;hostname=archlinux;pid=1;cwd=/home/mqj/WeKnora\x1b\\" +
		"\x1b]3008;start=68;type=shell;machineid=m;user=mqj;hostname=archlinux;pid=1;cwd=/tmp\x1b\\"
	p, ok := ParseOSC3008([]byte(raw))
	if !ok || p != "/tmp" {
		t.Fatalf("got %q ok=%v, want /tmp（取最后一个 type=shell 的 cwd）", p, ok)
	}

	only := "\x1b]3008;start=ea;type=command;cwd=/home/mqj/WeKnora\x1b\\"
	if p, ok := ParseOSC3008([]byte(only)); ok {
		t.Fatalf("type=command 的旧目录不得被采用，got %q", p)
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
