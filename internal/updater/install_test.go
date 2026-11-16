//go:build !darwin

package updater

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareZipSelectsOnlyExecutable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "update.zip")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	bad, _ := z.Create("../escape")
	bad.Write([]byte("unsafe"))
	good, _ := z.Create("ding-ssh.exe")
	good.Write([]byte("program"))
	z.Close()
	f.Close()
	got, e := Prepare(p)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(got)
	if string(b) != "program" {
		t.Fatalf("unexpected payload %q", b)
	}
	if _, e = os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(e) {
		t.Fatal("archive escaped destination")
	}
}
func TestPrepareRejectsMissingExecutable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "update.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	z.Create("other.exe")
	z.Close()
	f.Close()
	if _, e := Prepare(p); e == nil {
		t.Fatal("accepted archive without program")
	}
}
