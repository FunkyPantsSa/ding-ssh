package apphost

import (
	"ding-ssh/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestSSHImportAtomicGraph(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := &App{store: store.NewSQLiteStore(db)}
	file := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(file, []byte("Host jump\n HostName 192.0.2.1\nHost target\n HostName 10.0.0.2\n ProxyJump jump\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ImportSSHConfig(file, []SSHImportChoice{{"target", "copy"}}); err == nil {
		t.Fatal("missing dependency accepted")
	}
	nodes, err := app.store.List()
	if err != nil || len(nodes) != 0 {
		t.Fatalf("partial import: %v %v", nodes, err)
	}
	count, err := app.ImportSSHConfig(file, []SSHImportChoice{{"jump", "copy"}, {"target", "copy"}})
	if err != nil || count != 2 {
		t.Fatalf("import: %d %v", count, err)
	}
	nodes, err = app.store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Name == "target" {
			resolved, err := app.resolveSSHNode(node)
			if err != nil || len(resolved.ResolvedJumps) != 1 {
				t.Fatalf("chain: %+v %v", resolved, err)
			}
		}
	}
}
