package sshx

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveRemoteSymlinkDoesNotRemoveTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	writeFile(t, filepath.Join(target, "keep.txt"), []byte("keep"))
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("platform does not support symlinks: %v", err)
	}
	client := startInProcessSFTP(t, root)

	if err := removeRemote(client, "link"); err != nil {
		t.Fatalf("removeRemote(link): %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link should be removed, got err=%v", err)
	}
	assertFileContent(t, filepath.Join(target, "keep.txt"), []byte("keep"))
}

func TestRemoteReplacementPublishesStagedFileAndPreservesOriginalBeforeCommit(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	writeFile(t, target, []byte("original"))
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	client := startInProcessSFTP(t, root)

	temp, staged, err := openRemoteTempFile(client, "target.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(root, "target.txt"), []byte("original"))
	if err := replaceRemoteFile(client, temp, "target.txt"); err != nil {
		t.Fatalf("replaceRemoteFile: %v", err)
	}
	assertFileContent(t, filepath.Join(root, "target.txt"), []byte("new"))
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("replacement permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestLocalReplacementKeepsOriginalUntilCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	temp, staged, err := openLocalTempFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, []byte("original"))
	if err := replaceLocalFile(temp, target); err != nil {
		t.Fatalf("replaceLocalFile: %v", err)
	}
	assertFileContent(t, target, []byte("new"))
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("replacement permissions = %v, want 0600", info.Mode().Perm())
	}
}
