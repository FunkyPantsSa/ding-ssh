package debugsrv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotPrivateAtomicAndBounded(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link.png")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		if err = uiWriteFile(link, []byte("new")); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(target)
		if string(b) != "preserve" {
			t.Fatal("followed symlink")
		}
	}
	for i := 0; i < 66; i++ {
		if err := uiWriteFile(filepath.Join(dir, fmt.Sprintf("s%03d.png", i)), []byte("png")); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(dir, "expired.json")
	_ = uiWriteFile(old, []byte("{}"))
	_ = os.Chtimes(old, time.Now().Add(-25*time.Hour), time.Now().Add(-25*time.Hour))
	if err := pruneUISnapshots(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired file retained")
	}
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".png" {
			count++
			st, _ := e.Info()
			if st.Mode().Perm() != 0600 {
				t.Fatal("public snapshot")
			}
		}
	}
	if count > 64 {
		t.Fatalf("retained %d", count)
	}
}
