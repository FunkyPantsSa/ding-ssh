package sshx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderDownloadCannotFollowDestinationSymlinkOutsideSelection(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "root-link"
		if nested {
			name = "nested-link"
		}
		t.Run(name, func(t *testing.T) {
			remote := filepath.Join(t.TempDir(), "folder")
			local, outside := t.TempDir(), t.TempDir()
			remoteFile := filepath.Join(remote, "keep.txt")
			link := filepath.Join(local, "folder")
			if nested {
				remoteFile = filepath.Join(remote, "sub", "keep.txt")
				if err := os.MkdirAll(link, 0755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, "sub")
			}
			writeFile(t, remoteFile, []byte("remote-replacement"))
			writeFile(t, filepath.Join(outside, "keep.txt"), []byte("outside-original"))
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			client := startInProcessSFTP(t, filepath.Dir(remote))
			state := &sessionSFTP{client: client}
			state.once.Do(func() {})
			m := NewManager(func(string, interface{}) {})
			m.sessions["download"] = &Session{ID: "download", sftpState: state}
			if err := m.SftpDownloadFolder("download", remote, local); err == nil {
				t.Error("folder download followed a symlink outside the selected directory")
			}
			assertFileContent(t, filepath.Join(outside, "keep.txt"), []byte("outside-original"))
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary files leaked outside selection: entries=%d err=%v", len(entries), err)
			}
		})
	}
}
