package main

// M10 收尾：独立 SFTP 清理通道（宿主 op sudo.cleanup）的**宿主侧**用例。
//
// 覆盖两部分，**不连任何主机、不执行任何命令**：
//   - 入参校验与降级行为：路径形态（只认 /tmp/.ding-sudo-<12 位小写十六进制>）、远端写入白名单、
//     以及「拿不到 SFTP 客户端」时返回 verified=false + 中文原因（而**不是**退化成终端命令）；
//   - 「删除 + 复核」的真实协议路径：用 pkg/sftp 的**内存 SFTP 服务端**（InMemHandler）经管道
//     跑一遍真实的 Remove / Lstat 请求-响应，验证 verified 的判据（远端报 os.ErrNotExist）与幂等语义。
//
// 幂等语义在工具层的表现（removed / existed / verified 如何映射到 cleanupOnly 的返回、
// 超时批次如何取两条通道的或）由 internal/debugsrv/sudo_test.go 的替身用例覆盖。

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/models"
	"ding-ssh/internal/store"

	"github.com/pkg/sftp"
)

// newSudoCleanupHost 组装一个最小的 debugController：
// 设置存储里写入指定的远端写白名单，**故意不建会话管理器**（app.manager 为 nil）——
// 于是「宿主拿不到 SFTP 客户端」这条路可以在不连任何主机的前提下被覆盖。
func newSudoCleanupHost(t *testing.T, allowlist []string) *debugController {
	t.Helper()
	js, err := store.NewJSONSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	cur, err := js.Get()
	if err != nil {
		t.Fatalf("读取默认设置失败: %v", err)
	}
	d := models.DefaultDebugSettings()
	d.SFTPWriteAllowlist = allowlist
	cur.Debug = d
	if err := js.Save(cur); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	a := NewApp(DebugBoot{Enabled: true, Port: 0, Token: "test-token-sudo-cleanup"})
	a.settings = js
	return &debugController{app: a}
}

// 非法路径（形态不对 / 空）一律 ErrBadInput：这条 op 不能是任意删除原语。
func TestSudoHostCleanupRejectsBadPaths(t *testing.T) {
	c := newSudoCleanupHost(t, []string{"/tmp"})
	for _, p := range []string{
		"",
		"/etc/passwd",
		"/tmp/.ding-sudo-XYZ",
		"relative/path",
		"/tmp/../tmp/.ding-sudo-abcdef012345",
		"/tmp/.ding-sudo-abcdef01234",
		"/tmp/.ding-sudo-abcdef0123456",
		"/tmp/.ding-sudo-ABCDEF012345",
	} {
		_, err := c.sudoHostCleanup(context.Background(), map[string]any{"sessionId": "tab-1", "path": p})
		if err == nil {
			t.Fatalf("路径 %q 应被拒绝", p)
		}
		if !errors.Is(err, debugsrv.ErrBadInput) {
			t.Fatalf("路径 %q 的错误应是 ErrBadInput，实际：%v", p, err)
		}
	}
}

// 形态合法但不在白名单内 ⇒ 中文拒绝（白名单是第二条独立边界）。
func TestSudoHostCleanupRequiresAllowlist(t *testing.T) {
	c := newSudoCleanupHost(t, []string{"/srv/app"})
	_, err := c.sudoHostCleanup(context.Background(), map[string]any{
		"sessionId": "tab-1", "path": "/tmp/.ding-sudo-abcdef012345",
	})
	if err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("白名单不含 /tmp 时应拒绝并说明原因，实际：%v", err)
	}
}

// 拿不到 SFTP 客户端（这里用 nil 会话管理器模拟）⇒ 如实返回 verified=false + 中文原因，
// **不**报协议错误、**不**退化成终端命令（终端可能仍被挂起的作业占着）。
func TestSudoHostCleanupWithoutSftpReturnsUnverified(t *testing.T) {
	c := newSudoCleanupHost(t, []string{"/tmp"})
	v, err := c.sudoHostCleanup(context.Background(), map[string]any{
		"sessionId": "tab-1", "path": "/tmp/.ding-sudo-abcdef012345",
	})
	if err != nil {
		t.Fatalf("拿不到 SFTP 时应如实返回 verified=false（而不是报错）：%v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("返回类型 = %T，期望 map[string]any", v)
	}
	if m["verified"] != false || m["removed"] != false || m["existed"] != false {
		t.Fatalf("拿不到 SFTP 时三个布尔都应为 false：%v", m)
	}
	reason, _ := m["reason"].(string)
	if !strings.Contains(reason, "SFTP") || !strings.Contains(reason, "会话管理器") {
		t.Fatalf("应给出中文原因（说明拿不到 SFTP 客户端），实际：%q", reason)
	}
	if strings.Contains(reason, "terminal") || strings.Contains(reason, "rm -f") {
		t.Fatalf("不允许退化成终端命令：%q", reason)
	}
}

// 缺 path ⇒ ErrBadInput（宿主侧也要挡住空入参）。
func TestSudoHostCleanupRequiresPath(t *testing.T) {
	c := newSudoCleanupHost(t, []string{"/tmp"})
	_, err := c.sudoHostCleanup(context.Background(), map[string]any{"sessionId": "tab-1"})
	if err == nil || !errors.Is(err, debugsrv.ErrBadInput) {
		t.Fatalf("缺 path 应报 ErrBadInput，实际：%v", err)
	}
}

// isHostSudoOp 必须认领 sudo.cleanup（否则宿主会把请求当成未知 op）。
func TestIsHostSudoOpIncludesCleanup(t *testing.T) {
	for _, op := range []string{debugsrv.OpSudoStage, debugsrv.OpSudoScrub, debugsrv.OpSudoCleanup} {
		if !isHostSudoOp(op) {
			t.Fatalf("%s 应被 isHostSudoOp 认领", op)
		}
	}
	if isHostSudoOp("sudo.unknown") {
		t.Fatalf("未知 op 不应被认领")
	}
}

// ---- 「删除 + 复核」的真实协议路径（内存 SFTP 服务端，不连 SSH、不碰真实文件系统）----

// newInMemSFTPClient 用 pkg/sftp 的内存 SFTP 服务端（InMemHandler）接一个客户端：
// 走的是**真实的 SFTP 协议编解码**（管道 + 请求包），但不经过 SSH、也不碰任何真实路径。
//
// 为什么值得这么测：verified 的判据是「Remove 之后 Lstat 报 os.ErrNotExist」——
// 这个「远端报的错是不是 os.ErrNotExist」的映射是 pkg/sftp 的 normaliseError 做的，
// 只靠替身（自己返回 verified 布尔）是验不到的。
func newInMemSFTPClient(t *testing.T) *sftp.Client {
	t.Helper()
	srvConn, cliConn := net.Pipe()
	// 注意 NewRequestServer 不返回 error（参数非法会 panic），因此这里只需接一个 Serve。
	srv := sftp.NewRequestServer(srvConn, sftp.InMemHandler())
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(cliConn, cliConn)
	if err != nil {
		t.Fatalf("连接内存 SFTP 服务端失败: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.Close()
		_ = srv.Close()
		_ = srvConn.Close()
		_ = cliConn.Close()
	})
	return cli
}

func TestSudoSftpRemoveAndVerifySemantics(t *testing.T) {
	cli := newInMemSFTPClient(t)
	path := "/tmp/.ding-sudo-abcdef012345"

	// 1) 文件不存在 ⇒ 幂等成功：removed=false / existed=false / verified=true，且没有原因。
	removed, existed, verified, reason := sudoSftpRemoveAndVerify(cli, path)
	if removed || existed || !verified || reason != "" {
		t.Fatalf("文件不存在时应幂等成功（false/false/true，无原因），实际 %v/%v/%v/%q",
			removed, existed, verified, reason)
	}

	// 2) 写入一个含「密码」的文件（模拟 stage 之后的状态）⇒ 删除 + 复核三个布尔都为 true。
	//    内存服务端是空文件系统，先建 /tmp（与真实远端的 /tmp 对应）。
	if err := cli.Mkdir("/tmp"); err != nil {
		t.Fatalf("创建 /tmp 失败: %v", err)
	}
	f, err := cli.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	if _, err := f.Write([]byte("hunter2\n")); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭测试文件失败: %v", err)
	}
	removed, existed, verified, reason = sudoSftpRemoveAndVerify(cli, path)
	if !removed || !existed || !verified || reason != "" {
		t.Fatalf("文件存在时应删除并复核成功（true/true/true，无原因），实际 %v/%v/%v/%q",
			removed, existed, verified, reason)
	}
	// 复核的判据确实来自协议：客户端再 Lstat 一次应拿到 os.ErrNotExist
	if _, serr := cli.Lstat(path); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("删除后 Lstat 应报 os.ErrNotExist，实际：%v", serr)
	}

	// 3) 再清理一次 ⇒ 仍然幂等成功（重复 cleanupOnly 不报错）。
	removed, existed, verified, reason = sudoSftpRemoveAndVerify(cli, path)
	if removed || existed || !verified || reason != "" {
		t.Fatalf("重复清理应幂等成功，实际 %v/%v/%v/%q", removed, existed, verified, reason)
	}
}
