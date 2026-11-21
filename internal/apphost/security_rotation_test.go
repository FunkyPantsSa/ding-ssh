package apphost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/localterm"
	"ding-ssh/internal/store"
)

func TestAppMasterPasswordRotationRoundTripAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, op := range []string{"enable", "change", "disable"} {
			t.Run(op+map[bool]string{true: "-rollback", false: "-success"}[fail], func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv(appdir.EnvDevConfigDir, dir)
				app, db := newGroupTestApp(t)
				v, err := cryptox.OpenVault(filepath.Join(dir, "vault"))
				if err != nil {
					t.Fatal(err)
				}
				app.vault = v
				app.credentials = store.NewSQLiteCredentialStore(db)
				app.attachCipher()
				sealed, err := v.EncryptField("audit-secret-é中文")
				if err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"servers", "credentials"} {
					columns, values := "", ""
					if table == "servers" {
						columns, values = ",host", ",'127.0.0.1'"
					}
					if _, err := db.Exec("INSERT INTO "+table+"(id,name,password,key_content"+columns+") VALUES ('audit','audit',?,?"+values+")", sealed, sealed); err != nil {
						t.Fatal(err)
					}
				}
				if op != "enable" {
					if err := app.EnableMasterPassword("old"); err != nil {
						t.Fatal(err)
					}
				}
				if fail {
					if _, err := db.Exec("INSERT INTO credentials(id,name,password) VALUES ('broken','broken','enc1:broken')"); err != nil {
						t.Fatal(err)
					}
				}
				switch op {
				case "enable":
					err = app.EnableMasterPassword("new")
				case "change":
					err = app.ChangeMasterPassword("old", "new")
				case "disable":
					err = app.DisableMasterPassword("old")
				}
				if (err != nil) != fail {
					t.Fatalf("fail=%v err=%v", fail, err)
				}
				reopened, err := cryptox.OpenVault(filepath.Join(dir, "vault"))
				if err != nil {
					t.Fatal(err)
				}
				if reopened.Status().NeedsUnlock {
					pass := "new"
					if fail {
						pass = "old"
					}
					if err := reopened.Unlock(pass); err != nil {
						t.Fatal(err)
					}
				}
				for _, table := range []string{"servers", "credentials"} {
					var password, key string
					if err := db.QueryRow("SELECT password,key_content FROM "+table+" WHERE id='audit'").Scan(&password, &key); err != nil {
						t.Fatal(err)
					}
					for _, value := range []string{password, key} {
						if got, err := reopened.DecryptField(value); err != nil || got != "audit-secret-é中文" {
							t.Fatalf("%s data lost after %s: %v", table, op, err)
						}
					}
				}
			})
		}
	}
}

func TestDebugDisconnectRoutesLocalSession(t *testing.T) {
	// Use the platform shell but isolate its home and configuration files.
	t.Setenv("HOME", t.TempDir())
	m := localterm.NewManager(func(string, interface{}) {})
	t.Cleanup(m.CloseAll)
	if err := m.Connect("audit-local", "", 80, 24); err != nil {
		t.Fatal(err)
	}
	c := &debugController{app: &App{local: m}}
	raw, _ := json.Marshal(map[string]string{"id": "audit-local"})
	if _, err := c.Handle(context.Background(), debugsrv.OpSessionDisconnect, raw); err != nil {
		t.Fatal(err)
	}
	if m.Has("audit-local") {
		t.Fatal("debug disconnect left local PTY alive")
	}
}
