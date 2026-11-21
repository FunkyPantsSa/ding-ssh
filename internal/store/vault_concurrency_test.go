package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/models"
)

// Pause after the first field, outside the vault's per-field lock. This models
// a scheduler switch between password/key_content or between sealing and SQL.
type pausingVault struct {
	*cryptox.Vault
	decrypt bool
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (v *pausingVault) pause() { v.once.Do(func() { close(v.entered); <-v.resume }) }
func (v *pausingVault) EncryptField(value string) (string, error) {
	out, err := v.Vault.EncryptField(value)
	if !v.decrypt {
		v.pause()
	}
	return out, err
}
func (v *pausingVault) DecryptField(value string) (string, error) {
	out, err := v.Vault.DecryptField(value)
	if v.decrypt {
		v.pause()
	}
	return out, err
}

func TestVaultRotationWaitsForWholeStorageOperation(t *testing.T) {
	for _, operation := range []string{"save-server", "save-credential", "save-batch", "list-server", "list-credential"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(appdir.EnvDevConfigDir, dir)
			v, err := cryptox.OpenVault(filepath.Join(dir, "vault"))
			if err != nil {
				t.Fatal(err)
			}
			db, err := OpenSQLite(filepath.Join(dir, "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			servers, creds := NewSQLiteStore(db), NewSQLiteCredentialStore(db)
			servers.SetCipher(v)
			creds.SetCipher(v)
			node := models.ServerNode{ID: "saved", Name: "saved", Host: "127.0.0.1", Password: "password-é", KeyContent: "private-中文"}
			credential := models.Credential{ID: "saved", Name: "saved", Password: node.Password, KeyContent: node.KeyContent}
			if err := servers.Save(node); err != nil {
				t.Fatal(err)
			}
			if err := creds.Save(credential); err != nil {
				t.Fatal(err)
			}
			paused := &pausingVault{Vault: v, decrypt: operation == "list-server" || operation == "list-credential", entered: make(chan struct{}), resume: make(chan struct{})}
			servers.SetCipher(paused)
			creds.SetCipher(paused)
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "save-server":
					err = servers.Save(node)
				case "save-credential":
					err = creds.Save(credential)
				case "save-batch":
					err = servers.SaveBatch([]models.ServerNode{node})
				case "list-server":
					_, err = servers.List()
				case "list-credential":
					_, err = creds.List()
				}
				done <- err
			}()
			select {
			case <-paused.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("storage operation did not reach first field")
			}
			rotated := make(chan error, 1)
			go func() {
				_, _, err := v.EnableMasterPasswordWithMigration("next", func(oldKey, newKey []byte) error { return ReencryptAllSecrets(db, oldKey, newKey) })
				rotated <- err
			}()
			var early bool
			select {
			case err := <-rotated:
				early = true
				if err != nil {
					t.Error(err)
				}
				t.Error("rotation committed in the middle of a storage operation")
			case <-time.After(time.Second):
			}
			close(paused.resume)
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("storage operation deadlocked")
			}
			if !early {
				select {
				case err := <-rotated:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("key rotation deadlocked")
				}
			}
			gotServers, err := servers.List()
			if err != nil {
				t.Fatal("server became unreadable", err)
			}
			gotCreds, err := creds.List()
			if err != nil {
				t.Fatal("credential became unreadable", err)
			}
			if len(gotServers) != 1 || gotServers[0].Password != node.Password || gotServers[0].KeyContent != node.KeyContent || len(gotCreds) != 1 || gotCreds[0].Password != credential.Password || gotCreds[0].KeyContent != credential.KeyContent {
				t.Fatal("storage operation mixed keys")
			}
		})
	}
}
