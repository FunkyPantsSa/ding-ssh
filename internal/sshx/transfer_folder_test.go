package sshx

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
)

// ---- 纯函数：joinRemote / dirBytes / uploadFolderChunk ----

func TestJoinRemote(t *testing.T) {
	cases := []struct {
		base, name string
		want       string
	}{
		{"/", "myfolder", "/myfolder"},
		{"", "myfolder", "/myfolder"},
		{"/srv/app", "myfolder", "/srv/app/myfolder"},
		{"/srv/app/", "myfolder", "/srv/app/myfolder"},
		{"/srv/app", "a\\b.txt", "/srv/app/a/b.txt"},
		{"/", "/myfolder/", "/myfolder"},
	}
	for _, c := range cases {
		if got := joinRemote(c.base, c.name); got != c.want {
			t.Errorf("joinRemote(%q, %q) = %q, want %q", c.base, c.name, got, c.want)
		}
	}
}

func TestDirBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), []byte("hello"))             // 5
	writeFile(t, filepath.Join(root, "sub", "b.bin"), []byte("0123456789")) // 10
	writeFile(t, filepath.Join(root, "sub", "deep", "c"), []byte("0123456789"))

	total, err := dirBytes(root)
	if err != nil {
		t.Fatalf("dirBytes: %v", err)
	}
	if total != 25 {
		t.Fatalf("dirBytes = %d, want 25", total)
	}

	// 符号链接不计入（上传时同样跳过，避免链接循环）。
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Logf("平台不支持符号链接，跳过：%v", err)
		return
	}
	total, err = dirBytes(root)
	if err != nil {
		t.Fatalf("dirBytes with symlink: %v", err)
	}
	if total != 25 {
		t.Fatalf("dirBytes with symlink = %d, want 25", total)
	}
}

func TestCopyFolderChunkAccumulatesAndCancels(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 128*1024+17)
	var transferred atomic.Int64
	var dst bytes.Buffer

	// 正常拷贝：全部字节进入 dst，计数器累加到文件大小。
	err := copyFolderChunk(context.Background(), bytes.NewReader(payload), &dst, &transferred)
	if err != nil {
		t.Fatalf("copyFolderChunk: %v", err)
	}
	if dst.Len() != len(payload) {
		t.Fatalf("wrote %d, want %d", dst.Len(), len(payload))
	}
	if transferred.Load() != int64(len(payload)) {
		t.Fatalf("transferred = %d, want %d", transferred.Load(), len(payload))
	}

	// 已取消的上下文：什么都不写，立即返回 ErrTransferCancelled。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dst2 bytes.Buffer
	var t2 atomic.Int64
	err = copyFolderChunk(ctx, bytes.NewReader(payload), &dst2, &t2)
	if !errors.Is(err, ErrTransferCancelled) {
		t.Fatalf("cancelled ctx err = %v, want ErrTransferCancelled", err)
	}
	if dst2.Len() != 0 || t2.Load() != 0 {
		t.Fatalf("cancelled copy should not write: dst=%d transferred=%d", dst2.Len(), t2.Load())
	}
}

// ---- 集成：uploadFolderTree 走真实进程内 sftp 服务端 ----

// startInProcessSFTP 启动一个基于临时目录的进程内 sftp 服务端，返回客户端。
func startInProcessSFTP(t *testing.T, serverRoot string) *sftp.Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server, err := sftp.NewServer(serverConn, sftp.WithServerWorkingDirectory(serverRoot))
	if err != nil {
		t.Fatalf("sftp.NewServer: %v", err)
	}
	go func() { _ = server.Serve() }()
	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatalf("sftp.NewClientPipe: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	return client
}

// TestUploadFolderTreeRecursesIntoServerFS 把本地文件夹递归上传到进程内 sftp 服务端，
// 验证目录结构、文件内容与累计字节数。
func TestUploadFolderTreeRecursesIntoServerFS(t *testing.T) {
	localRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "myfolder", "sub", "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(localRoot, "myfolder", "sub", "b.txt"), []byte("beta"))
	writeFile(t, filepath.Join(localRoot, "myfolder", "top.bin"), bytes.Repeat([]byte{0xAB}, 2048))
	// 空子目录也要在远端出现。
	if err := os.MkdirAll(filepath.Join(localRoot, "myfolder", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	serverRoot := t.TempDir()
	client := startInProcessSFTP(t, serverRoot)

	// 与 SftpUploadFolder 的真实流程一致：先创建远端根目录。
	if err := client.MkdirAll("dst/myfolder"); err != nil {
		t.Fatalf("MkdirAll(dst/myfolder): %v", err)
	}

	m := NewManager(nil)
	var transferred atomic.Int64
	err := m.uploadFolderTree(context.Background(), client, filepath.Join(localRoot, "myfolder"), "dst/myfolder", &transferred)
	if err != nil {
		t.Fatalf("uploadFolderTree: %v", err)
	}

	assertFileContent(t, filepath.Join(serverRoot, "dst", "myfolder", "sub", "a.txt"), []byte("alpha"))
	assertFileContent(t, filepath.Join(serverRoot, "dst", "myfolder", "sub", "b.txt"), []byte("beta"))
	assertFileContent(t, filepath.Join(serverRoot, "dst", "myfolder", "top.bin"), bytes.Repeat([]byte{0xAB}, 2048))
	if fi, err := os.Stat(filepath.Join(serverRoot, "dst", "myfolder", "empty")); err != nil || !fi.IsDir() {
		t.Fatalf("远端空目录应存在且为目录: err=%v fi=%v", err, fi)
	}

	want := int64(5 + 4 + 2048)
	if transferred.Load() != want {
		t.Fatalf("transferred = %d, want %d", transferred.Load(), want)
	}
}

// TestUploadFolderTreeSkipsSymlink 确认符号链接会被跳过而不是跟随。
func TestUploadFolderTreeSkipsSymlink(t *testing.T) {
	localRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "myfolder", "real.txt"), []byte("real"))
	if err := os.Symlink(filepath.Join(localRoot, "myfolder", "real.txt"), filepath.Join(localRoot, "myfolder", "link.txt")); err != nil {
		t.Skipf("platform does not support symlink: %v", err)
	}

	serverRoot := t.TempDir()
	client := startInProcessSFTP(t, serverRoot)
	if err := client.MkdirAll("dst/myfolder"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	m := NewManager(nil)
	var transferred atomic.Int64
	if err := m.uploadFolderTree(context.Background(), client, filepath.Join(localRoot, "myfolder"), "dst/myfolder", &transferred); err != nil {
		t.Fatalf("uploadFolderTree: %v", err)
	}
	if transferred.Load() != 4 {
		t.Fatalf("transferred = %d, want 4（符号链接不计入）", transferred.Load())
	}
	if _, err := os.Stat(filepath.Join(serverRoot, "dst", "myfolder", "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("远端不应出现链接文件: err=%v", err)
	}
}

// TestUploadFolderTreeCancelled 已取消的上下文会让遍历立即停下。
func TestUploadFolderTreeCancelled(t *testing.T) {
	localRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "myfolder", "a.txt"), []byte("a"))

	serverRoot := t.TempDir()
	client := startInProcessSFTP(t, serverRoot)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := NewManager(nil)
	var transferred atomic.Int64
	err := m.uploadFolderTree(ctx, client, filepath.Join(localRoot, "myfolder"), "dst/myfolder", &transferred)
	if !errors.Is(err, ErrTransferCancelled) {
		t.Fatalf("err = %v, want ErrTransferCancelled", err)
	}
}

// ---- 下载文件夹 ----

// TestRemoteDirBytes 统计远端目录树内常规文件总字节数（符号链接不计入）。
func TestRemoteDirBytes(t *testing.T) {
	serverRoot := t.TempDir()
	writeFile(t, filepath.Join(serverRoot, "myfolder", "sub", "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(serverRoot, "myfolder", "top.bin"), bytes.Repeat([]byte{0x11}, 1000))
	writeFile(t, filepath.Join(serverRoot, "myfolder", "sub", "deep", "c.txt"), []byte("c"))

	client := startInProcessSFTP(t, serverRoot)
	total, err := remoteDirBytes(client, "myfolder")
	if err != nil {
		t.Fatalf("remoteDirBytes: %v", err)
	}
	if total != 5+1000+1 {
		t.Fatalf("total = %d, want %d", total, 5+1000+1)
	}
}

// TestDownloadFolderTreeRecursesFromServerFS 把进程内 sftp 服务端上的远端文件夹
// 递归下载到本地，验证目录结构、文件内容与累计字节数。
func TestDownloadFolderTreeRecursesFromServerFS(t *testing.T) {
	serverRoot := t.TempDir()
	writeFile(t, filepath.Join(serverRoot, "myfolder", "sub", "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(serverRoot, "myfolder", "sub", "b.txt"), []byte("beta"))
	writeFile(t, filepath.Join(serverRoot, "myfolder", "top.bin"), bytes.Repeat([]byte{0xCD}, 3000))
	// 空子目录也要在本地出现。
	if err := os.MkdirAll(filepath.Join(serverRoot, "myfolder", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	client := startInProcessSFTP(t, serverRoot)

	localTarget := t.TempDir()
	// 与 SftpDownloadFolder 的真实流程一致：先创建本地根目录。
	if err := os.MkdirAll(filepath.Join(localTarget, "myfolder"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(nil)
	var transferred atomic.Int64
	err := m.downloadFolderTree(context.Background(), client, "myfolder", filepath.Join(localTarget, "myfolder"), &transferred)
	if err != nil {
		t.Fatalf("downloadFolderTree: %v", err)
	}

	assertFileContent(t, filepath.Join(localTarget, "myfolder", "sub", "a.txt"), []byte("alpha"))
	assertFileContent(t, filepath.Join(localTarget, "myfolder", "sub", "b.txt"), []byte("beta"))
	assertFileContent(t, filepath.Join(localTarget, "myfolder", "top.bin"), bytes.Repeat([]byte{0xCD}, 3000))
	if fi, err := os.Stat(filepath.Join(localTarget, "myfolder", "empty")); err != nil || !fi.IsDir() {
		t.Fatalf("本地空目录应存在且为目录: err=%v fi=%v", err, fi)
	}

	want := int64(5 + 4 + 3000)
	if transferred.Load() != want {
		t.Fatalf("transferred = %d, want %d", transferred.Load(), want)
	}
}

// TestDownloadFolderTreeCancelled 已取消的上下文会让下载立即停下。
func TestDownloadFolderTreeCancelled(t *testing.T) {
	serverRoot := t.TempDir()
	writeFile(t, filepath.Join(serverRoot, "myfolder", "a.txt"), []byte("a"))

	client := startInProcessSFTP(t, serverRoot)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := NewManager(nil)
	var transferred atomic.Int64
	err := m.downloadFolderTree(ctx, client, "myfolder", filepath.Join(t.TempDir(), "myfolder"), &transferred)
	if !errors.Is(err, ErrTransferCancelled) {
		t.Fatalf("err = %v, want ErrTransferCancelled", err)
	}
}

// ---- helpers ----

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s 内容 = %q, want %q", path, got, want)
	}
}
