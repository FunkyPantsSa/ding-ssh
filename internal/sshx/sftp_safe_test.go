package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteWriteCanonicalBoundary(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "secret"), []byte("keep"))
	if err := os.Symlink(outside, filepath.Join(allowed, "escape")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(allowed, "link")); err != nil {
		t.Fatal(err)
	}
	client := startInProcessSFTP(t, root)
	roots := []string{allowed}
	for _, target := range []string{filepath.Join(allowed, "escape", "secret"), filepath.Join(allowed, "escape", "new", "file"), filepath.Join(allowed, "link")} {
		if err := ValidateRemoteWritePath(client, roots, target, false); err == nil {
			t.Fatalf("escaped target accepted: %s", target)
		}
		if _, err := WriteRemoteFile(context.Background(), client, target, []byte("bad"), RemoteWriteOptions{Allowlist: roots, CreateDirs: true}); err == nil {
			t.Fatal("escaped write accepted")
		}
	}
	if err := ValidateRemoteWritePath(client, roots, filepath.Join(allowed, "link"), true); err != nil {
		t.Fatalf("unlink own link should be allowed: %v", err)
	}
	if err := ValidateRemoteReadPath(client, roots, filepath.Join(allowed, "escape", "secret")); err == nil {
		t.Fatal("scoped read escaped")
	}
	assertFileContent(t, filepath.Join(outside, "secret"), []byte("keep"))
}

func TestSafeRemoteWriteConditionalAtomicAndAppend(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file")
	writeFile(t, target, []byte("old"))
	if err := os.Chmod(target, 0600); err != nil {
		t.Fatal(err)
	}
	client := startInProcessSFTP(t, root)
	sum := sha256.Sum256([]byte("old"))
	expected := hex.EncodeToString(sum[:])
	ctx := context.Background()
	_, err := WriteRemoteFile(ctx, client, target, []byte("bad"), RemoteWriteOptions{Allowlist: []string{root}, ExpectedHash: strings.Repeat("0", 64)})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("stale write %v", err)
	}
	assertFileContent(t, target, []byte("old"))
	result, err := WriteRemoteFile(ctx, client, target, []byte("new"), RemoteWriteOptions{Allowlist: []string{root}, ExpectedHash: expected})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Atomic || result.Bytes != 3 {
		t.Fatalf("bad result %#v", result)
	}
	assertFileContent(t, target, []byte("new"))
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatal("mode changed")
	}
	_, err = WriteRemoteFile(ctx, client, target, []byte("replayed"), RemoteWriteOptions{Allowlist: []string{root}, ExpectedHash: expected})
	if err == nil {
		t.Fatal("stale retry overwrote new content")
	}
	result, err = WriteRemoteFile(ctx, client, target, []byte("!"), RemoteWriteOptions{Allowlist: []string{root}, Append: true})
	if err != nil || result.Atomic {
		t.Fatalf("append %#v %v", result, err)
	}
	assertFileContent(t, target, []byte("new!"))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ding-ssh-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestSafeRemoteWriteCancelledPreservesTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file")
	writeFile(t, target, []byte("keep"))
	client := startInProcessSFTP(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WriteRemoteFile(ctx, client, target, []byte("bad"), RemoteWriteOptions{Allowlist: []string{root}}); err == nil {
		t.Fatal("cancelled write accepted")
	}
	assertFileContent(t, target, []byte("keep"))
	if _, err := WriteRemoteFile(context.Background(), client, target, []byte("bad"), RemoteWriteOptions{Allowlist: []string{root}, ExpectedHash: "missing"}); err == nil {
		t.Fatal("create-only overwrote target")
	}
}

func TestCreateRemotePrivateFile(t *testing.T) {
	root := t.TempDir()
	client := startInProcessSFTP(t, root)
	target := filepath.Join(root, "credential")
	payload := []byte("password\n")
	if n, err := CreateRemotePrivateFile(context.Background(), client, target, payload, []string{root}); err != nil || n != len(payload) {
		t.Fatalf("private create: bytes=%d err=%v", n, err)
	}
	assertFileContent(t, target, payload)
	fi, err := os.Stat(target)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("private permissions: %v %v", fi, err)
	}
	if _, err := CreateRemotePrivateFile(context.Background(), client, target, []byte("replacement"), []string{root}); err == nil {
		t.Fatal("overwrote existing credential")
	}
	assertFileContent(t, target, payload)
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := CreateRemotePrivateFile(context.Background(), client, link, []byte("replacement"), []string{root}); err == nil {
		t.Fatal("followed existing credential symlink")
	}
	assertFileContent(t, target, payload)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	missing := filepath.Join(root, "cancelled")
	if _, err := CreateRemotePrivateFile(ctx, client, missing, payload, []string{root}); err == nil {
		t.Fatal("created credential after cancellation")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("cancelled credential exists: %v", err)
	}
}

func TestRemoteCreateDirsCannotCreateAncestorsOutsideScope(t *testing.T) {
	root := t.TempDir()
	client := startInProcessSFTP(t, root)
	missingParent := filepath.Join(root, "outside")
	allowed := filepath.Join(missingParent, "allowed")
	target := filepath.Join(allowed, "file")
	if _, err := WriteRemoteFile(context.Background(), client, target, []byte("data"), RemoteWriteOptions{CreateDirs: true, Allowlist: []string{allowed}}); err == nil {
		t.Fatal("created an ancestor outside the authorized root")
	}
	if _, err := os.Stat(missingParent); !os.IsNotExist(err) {
		t.Fatalf("unauthorized ancestor created: %v", err)
	}
	allowed = filepath.Join(root, "new-authorized-root")
	target = filepath.Join(allowed, "nested", "file")
	if _, err := WriteRemoteFile(context.Background(), client, target, []byte("data"), RemoteWriteOptions{CreateDirs: true, Allowlist: []string{allowed}}); err != nil {
		t.Fatalf("could not create within authorized root: %v", err)
	}
	assertFileContent(t, target, []byte("data"))
}

func TestRemotePatchPreviewVersionConflictAndCommit(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "config")
	writeFile(t, target, []byte("port=22\nname=中文\n"))
	client := startInProcessSFTP(t, root)
	ctx := context.Background()
	preview, err := PatchRemoteFile(ctx, client, target, "port=22", "port=2222", 1, "", true, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || preview.ExpectedHash == "" || preview.NewHash == preview.ExpectedHash {
		t.Fatalf("preview %#v", preview)
	}
	assertFileContent(t, target, []byte("port=22\nname=中文\n"))
	if _, err := PatchRemoteFile(ctx, client, target, "port=22", "port=2222", 2, preview.ExpectedHash, false, []string{root}); err == nil {
		t.Fatal("wrong match count applied")
	}
	writeFile(t, target, []byte("port=22\nname=someone-else\n"))
	if _, err := PatchRemoteFile(ctx, client, target, "port=22", "port=2222", 1, preview.ExpectedHash, false, []string{root}); err == nil {
		t.Fatal("stale preview applied")
	}
	assertFileContent(t, target, []byte("port=22\nname=someone-else\n"))
	preview, err = PatchRemoteFile(ctx, client, target, "port=22", "port=2222", 1, "", true, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := PatchRemoteFile(ctx, client, target, "port=22", "port=2222", 1, preview.ExpectedHash, false, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Atomic {
		t.Fatalf("commit %#v", result)
	}
	assertFileContent(t, target, []byte("port=2222\nname=someone-else\n"))
}

func TestRemotePatchRefusesAmbiguityBinaryAndExpansion(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file")
	client := startInProcessSFTP(t, root)
	ctx := context.Background()
	writeFile(t, target, []byte("aa"))
	if _, err := PatchRemoteFile(ctx, client, target, "a", "b", 1, "", true, []string{root}); err == nil {
		t.Fatal("ambiguous replacement accepted")
	}
	if _, err := PatchRemoteFile(ctx, client, target, "a", strings.Repeat("b", 1<<20), 2, "", true, []string{root}); err == nil {
		t.Fatal("oversized expansion accepted")
	}
	writeFile(t, target, []byte{0xff, 0x00, 'a'})
	if _, err := PatchRemoteFile(ctx, client, target, "a", "b", 1, "", true, []string{root}); err == nil {
		t.Fatal("binary patch accepted")
	}
	writeFile(t, target, []byte{'a', 0x00, 'b'})
	if _, err := PatchRemoteFile(ctx, client, target, "a", "b", 1, "", true, []string{root}); err == nil {
		t.Fatal("NUL-containing binary patch accepted")
	}
}
