package store

import (
	"bytes"
	"path/filepath"
	"testing"

	"ding-ssh/internal/cryptox"
)

func TestReencryptAllSecretsIsAtomicOnCredentialFailure(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldKey, newKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	sealed, err := cryptox.Encrypt(oldKey, "server-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO servers(id,name,host,password) VALUES ('s','s','localhost',?)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO credentials(id,name,password) VALUES ('c','c','enc1:corrupted')`); err != nil {
		t.Fatal(err)
	}
	if err := ReencryptAllSecrets(db, oldKey, newKey); err == nil {
		t.Fatal("expected corrupted credential failure")
	}
	var got string
	if err := db.QueryRow(`SELECT password FROM servers WHERE id='s'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != sealed {
		t.Fatal("a failed credential rotation already rewrote server secrets; keys are now mixed")
	}
}
