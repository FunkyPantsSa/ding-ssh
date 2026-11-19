package sshx

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ding-ssh/internal/models"
	"github.com/pkg/sftp"
)

type dirEvent struct {
	path string
	src  string
}

func newDirTestSession(lastDir string) (*Session, *[]dirEvent) {
	events := &[]dirEvent{}
	s := &Session{
		ID:        "sess-test",
		server:    models.ServerNode{User: "root"},
		home:      "/root",
		lastDir:   lastDir,
		verifyDir: func(string) (bool, bool) { return true, true }, // 默认视作目录存在，聚焦解析逻辑
	}
	s.onDirSync = func(_ string, p string) {
		*events = append(*events, dirEvent{path: p})
	}
	return s, events
}

// feedDirChunks 模拟 pump 的增量投喂：每次把一块输出追加到滚动缓冲后解析一次。
func feedDirChunks(s *Session, acc, carry *[]byte, chunks ...string) {
	for _, c := range chunks {
		*acc = append(*acc, c...)
		if len(*acc) > 64*1024 {
			*acc = (*acc)[len(*acc)-64*1024:]
		}
		candidates, next := s.resolveDirChange(*acc, *carry, []byte(c))
		*carry = next
		for _, c := range candidates {
			s.processDirJob(dirJob{dirCandidate: c})
		}
	}
}

func assertDirEvents(t *testing.T, got *[]dirEvent, want ...string) {
	t.Helper()
	if len(*got) != len(want) {
		t.Fatalf("事件 = %+v, want %v", *got, want)
	}
	for i, w := range want {
		if (*got)[i].path != w {
			t.Fatalf("事件[%d] = %q, want %q（全部：%+v）", i, (*got)[i].path, w, *got)
		}
	}
}

// 回归：CentOS 默认 \W 提示符（只显示目录名）时，提示符解析不出绝对路径，
// 只能依赖 cd 回显。此前每个新 chunk 都会重新命中历史回显，相对参数再以已更新的
// lastDir 为基准拼接，路径被叠加成 /root/install/install/...（现象见 issue 日志）。
func TestDirSyncConsumesRelativeCdEchoOnce(t *testing.T) {
	s, events := newDirTestSession("/root")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host ~]# cd install\r\n",
		"[root@host install]# ",
		"l", "s", "\r\n",
		"bin\r\n", "conf\r\n", "logs\r\n",
		"[root@host install]# ",
	)
	assertDirEvents(t, events, "/root/install")

	// 进入下一层目录同样只应产生一条事件
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd export_rpm\r\n",
		"[root@host export_rpm]# ",
		"a\r\n", "b\r\n",
	)
	assertDirEvents(t, events, "/root/install", "/root/install/export_rpm")

	if s.currentDir() != "/root/install/export_rpm" {
		t.Fatalf("lastDir = %q", s.currentDir())
	}
}

// 相对参数（cd ..、cd sub）按"命令执行前"的目录解析一次，不随输出重复推进。
func TestDirSyncRelativeCdAppliedOnce(t *testing.T) {
	s, events := newDirTestSession("/root/install")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd ..\r\n",
		"[root@host ~]# ",
		"x\r\n", "y\r\n", "z\r\n",
	)
	assertDirEvents(t, events, "/root")
}

// 同一条 cd 回显被拆到多个 Read 块时，只补齐成行后消费一次。
func TestDirSyncCdEchoSplitAcrossChunks(t *testing.T) {
	s, events := newDirTestSession("/root/install")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd su",
		"bdir\r\n",
		"[root@host subdir]# ",
	)
	assertDirEvents(t, events, "/root/install/subdir")
}

// 一次到达多条 cd 回显（粘贴多行命令）时按顺序逐条上报，基准依次推进。
func TestDirSyncMultipleCdLinesInOneChunk(t *testing.T) {
	s, events := newDirTestSession("/root")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host ~]# cd a\r\n[root@host a]# cd b\r\n",
	)
	assertDirEvents(t, events, "/root/a", "/root/a/b")
}

// cd - / cd . 不构成目录变更。
func TestDirSyncIgnoresCdDashAndDot(t *testing.T) {
	s, events := newDirTestSession("/root")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host ~]# cd -\r\n",
		"[root@host ~]# cd .\r\n",
		"[root@host ~]# ",
	)
	assertDirEvents(t, events)
}

// 提示符给出绝对路径时优先采用；重复刷新的提示符不会重复上报。
func TestDirSyncPromptPriorityAndDedup(t *testing.T) {
	s, events := newDirTestSession("/root")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host /var/log]# ",
		"[root@host /var/log]# ",
		"[root@host /var/log]# ",
	)
	assertDirEvents(t, events, "/var/log")

	cands, _ := s.resolveDirChange([]byte("[root@host /tmp]# "), nil, nil)
	if len(cands) != 1 || cands[0].value != "/tmp" || cands[0].src != "prompt" {
		t.Fatalf("got %+v, want /tmp src=prompt", cands)
	}
}

// 脚本输出里独立成行的绝对路径 cd 也只消费一次。
func TestDirSyncBareCdLineConsumedOnce(t *testing.T) {
	s, events := newDirTestSession("/root")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"cd /data\r\n",
		"file1\r\n", "file2\r\n",
		"cd /opt\r\n",
	)
	assertDirEvents(t, events, "/data", "/opt")
}

// 失败的相对 cd（目录不存在）不上报，且不污染后续相对 cd 的解析基准。
func TestDirSyncSkipsMissingCdTarget(t *testing.T) {
	s, events := newDirTestSession("/root/install")
	s.verifyDir = func(dir string) (bool, bool) {
		if dir == "/root/install/install" {
			return false, true // 远端明确返回 no such file
		}
		return true, true
	}
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd install\r\n", // 已在 install 里再 cd install，会失败
		"bash: cd: install: No such file or directory\r\n",
		"[root@host install]# ",
	)
	assertDirEvents(t, events)
	if s.currentDir() != "/root/install" {
		t.Fatalf("失败的命令不应改变目录，lastDir=%q", s.currentDir())
	}

	// 后续 cd .. 仍以真实目录 /root/install 为基准
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd ..\r\n",
		"[root@host ~]# ",
	)
	assertDirEvents(t, events, "/root")
}

// cd 的目标是文件（或不存在）时同样不上报；绝对路径的 cd 也要校验。
func TestDirSyncVerifiesAllCdTargets(t *testing.T) {
	s, events := newDirTestSession("/root")
	s.verifyDir = func(dir string) (bool, bool) {
		switch dir {
		case "/root", "/data", "/root/sub":
			return true, true
		default:
			return false, true
		}
	}
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"cd /missing\r\n",
		"[root@host ~]# cd sub\r\n",
		"[root@host sub]# cd /data\r\n",
	)
	assertDirEvents(t, events, "/root/sub", "/data")
}

// 校验不可用（SFTP 被禁用、超时等）时退回旧行为：按回显直接同步，保证功能可用。
func TestDirSyncFallsBackWhenVerifyUnavailable(t *testing.T) {
	s, events := newDirTestSession("/root")
	s.verifyDir = func(string) (bool, bool) { return false, false } // known=false
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host ~]# cd install\r\n",
		"[root@host install]# ",
	)
	assertDirEvents(t, events, "/root/install")
}

// 串行协程：事件入队后按顺序处理并上报，重连/关闭时能停止。
func TestDirSyncWorkerProcessesQueue(t *testing.T) {
	s, _ := newDirTestSession("/root")
	events := make(chan string, 4)
	s.onDirSync = func(_ string, p string) { events <- p }
	s.startDirSync()
	defer s.stopDirSync()

	s.enqueueDir(dirCandidate{value: "a", src: "cd"})
	s.enqueueDir(dirCandidate{value: "b", src: "cd"}) // 应基于 a 解析成 /root/a/b
	for _, want := range []string{"/root/a", "/root/a/b"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("事件 = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("等待目录事件超时（want %q）", want)
		}
	}

	// 停止后入队不应 panic，也不应再有事件
	s.stopDirSync()
	s.enqueueDir(dirCandidate{value: "/tmp", src: "prompt"})
	select {
	case got := <-events:
		t.Fatalf("停止后仍收到事件 %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// 用进程内 SFTP 服务验证存在性校验的错误映射：目录 -> true，文件 -> false，
// 不存在 -> false 且 known=true（只有拿到确定结论时才抑制上报）。
func TestRemoteDirExists(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	serverEnd, clientEnd := net.Pipe()
	srv, err := sftp.NewServer(serverEnd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Close()
		_ = serverEnd.Close()
		_ = clientEnd.Close()
	})
	go func() { _ = srv.Serve() }()

	c, err := sftp.NewClientPipe(clientEnd, clientEnd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	state := &sessionSFTP{}
	state.once.Do(func() {}) // 标记已初始化，直接注入测试客户端
	state.client = c
	s := &Session{ID: "sftp-test", sftpState: state}

	cases := []struct {
		path       string
		wantExists bool
		wantKnown  bool
	}{
		{dir, true, true},
		{file, false, true},
		{filepath.Join(root, "nope"), false, true},
	}
	for _, c := range cases {
		exists, known := s.remoteDirExists(c.path)
		if exists != c.wantExists || known != c.wantKnown {
			t.Errorf("remoteDirExists(%q) = (%v,%v), want (%v,%v)", c.path, exists, known, c.wantExists, c.wantKnown)
		}
	}
}

// SFTP 与 Shell 目录视图不一致（如 chroot 的 sftp-server）时，不能把所有 cd 都当成
// 失败吞掉：当前目录在 SFTP 视角也 Stat 不到即判定视图不一致，停用校验并退回乐观上报。
func TestDirSyncDisablesVerifyWhenSftpViewDiffers(t *testing.T) {
	s, events := newDirTestSession("/root/install")
	s.verifyDir = func(string) (bool, bool) { return false, true } // SFTP 视角什么都看不到
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host install]# cd export_rpm\r\n",
		"[root@host export_rpm]# cd ..\r\n",
	)
	assertDirEvents(t, events, "/root/install/export_rpm", "/root/install")
}

// 校验超时进入冷却后直接按"无法判断"处理，不再发起 SFTP 请求。
func TestRemoteDirExistsSkipsDuringCooldown(t *testing.T) {
	s := &Session{ID: "sess-test"}
	s.dirVerifyQuietUntil = time.Now().Add(time.Minute)
	called := false
	s.verifyDir = func(string) (bool, bool) {
		called = true
		return true, true
	}
	if exists, known := s.remoteDirExists("/anywhere"); exists || known {
		t.Fatalf("冷却期内应返回 (false,false)，got (%v,%v)", exists, known)
	}
	if called {
		t.Fatal("冷却期内不应调用远端校验")
	}
}

// 无参数 cd（回到 home）：提示符同行回显与脚本裸行都能识别，且同样走存在性校验。
func TestDirSyncBareCdGoesHome(t *testing.T) {
	s, events := newDirTestSession("/opt")
	var acc, carry []byte
	feedDirChunks(s, &acc, &carry,
		"[root@host /data]# ",   // 绝对提示符 → /data
		"\r\n",                  // 结束提示符行
		"cd \t\r\n",             // 脚本裸行 cd → home
		"[root@host ~]# cd\r\n", // 提示符同行裸 cd → home（去重）
	)
	assertDirEvents(t, events, "/data", "/root")
	if s.currentDir() != "/root" {
		t.Fatalf("lastDir = %q, want /root", s.currentDir())
	}
}
