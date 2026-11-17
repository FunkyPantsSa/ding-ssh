package store

import (
	"ding-ssh/internal/models"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSSHOptionsSQLiteMigrationRoundTrip(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewSQLiteStore(db)
	old := models.ServerNode{ID: "legacy", Name: "legacy", AuthType: "password", Password: "secret"}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	options := models.SSHOptions{JumpServerIDs: []string{"jump"}, IdentityFiles: []string{"~/.ssh/key"}, IdentityAgent: "agent-path", IdentitiesOnly: true, HostKeyAlias: "alias", KnownHostsSources: []string{"known"}, ImportAlias: "source", ImportSource: "config"}
	node := models.ServerNode{ID: "new", Name: "new", AuthType: "agent", SSHOptions: options}
	if err := store.SaveBatch([]models.ServerNode{node}); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatal(list)
	}
	for _, got := range list {
		if got.ID == "legacy" && got.Password != "secret" {
			t.Fatal("legacy credential changed")
		}
		if got.ID == "new" && !reflect.DeepEqual(got.SSHOptions, options) {
			t.Fatalf("options lost: %+v", got)
		}
	}
}
