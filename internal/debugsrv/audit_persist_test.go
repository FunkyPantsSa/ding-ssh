package debugsrv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPersistentAuditMetadataAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAuditLog()
	if err := a.ConfigurePersistence(path); err != nil {
		t.Fatal(err)
	}
	a.Append(AuditRecord{Source: "mcp", Tool: "read_terminal", ClientID: "client_a", OperationID: "op_a", Args: `{"innocent":"secret-raw-command"}`, Error: "secret-raw-error", OK: false})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-raw") || strings.Contains(string(b), "args") || strings.Contains(string(b), "error") {
		t.Fatalf("raw payload persisted: %s", b)
	}
	if !strings.Contains(string(b), `"clientId":"client_a"`) {
		t.Fatal("missing identity")
	}
	st, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", st.Mode())
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(auditFileLimit); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	a.Append(AuditRecord{Tool: "send_input", OK: true})
	if _, err = os.Stat(path + ".1"); err != nil {
		t.Fatal("not rotated", err)
	}
	if err = a.ConfigurePersistence(""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	a.Append(AuditRecord{Tool: "send_input"})
	after, _ := os.Stat(path)
	if before.Size() != after.Size() {
		t.Fatal("disabled writer still writes")
	}
}

func TestPersistentAuditRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "audit")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if err := NewAuditLog().ConfigurePersistence(link); err == nil {
		t.Fatal("accepted symlink")
	}
}
