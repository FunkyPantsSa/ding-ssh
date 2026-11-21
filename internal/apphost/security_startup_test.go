package apphost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/models"
)

func TestVaultInitializationFailureBlocksSecretWrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appdir.EnvDevConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "security.json"), []byte(`{"masterPasswordEnabled":true,"saltB64":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(DebugBoot{})
	app.Startup(context.Background())
	t.Cleanup(func() {
		if app.db != nil {
			_ = app.db.Close()
		}
		app.manager.CloseAll()
		app.local.CloseAll()
	})
	if app.vault != nil {
		t.Fatal("fixture did not fail vault initialization")
	}
	if status := app.GetSecurityStatus(); status.Unlocked || !status.NeedsUnlock || status.InitializationError == "" {
		t.Errorf("failed vault is not reported as blocked: %+v", status)
	}
	if _, err := app.SaveCredential(models.Credential{ID: "blocked", Name: "blocked", Password: "must-not-persist"}); err == nil {
		t.Error("credential write accepted without a usable vault")
	}
	if _, err := app.SaveServer(models.ServerNode{ID: "blocked", Name: "blocked", Host: "127.0.0.1", Port: 22, Password: "must-not-persist"}); err == nil {
		t.Error("server write accepted without a usable vault")
	}
	for _, table := range []string{"servers", "credentials"} {
		var count int
		if err := app.db.QueryRow("SELECT count(*) FROM " + table + " WHERE id='blocked'").Scan(&count); err != nil || count != 0 {
			t.Errorf("%s persisted a secret after initialization failed: count=%d err=%v", table, count, err)
		}
	}
	if err := app.DeleteServer("blocked"); err == nil {
		t.Error("server deletion accepted after vault failure")
	}
	if err := app.DeleteCredential("blocked"); err == nil {
		t.Error("credential deletion accepted after vault failure")
	}
}
