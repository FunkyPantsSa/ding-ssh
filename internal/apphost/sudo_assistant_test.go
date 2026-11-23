package apphost

import (
	"ding-ssh/internal/models"
	"ding-ssh/internal/store"
	"path/filepath"
	"testing"
)

func TestSavedSudoPasswordUsesLatestBoundServerAndNeverKeyPhrase(t *testing.T) {
	st, err := store.NewJSONStore(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st}
	connected := models.ServerNode{ID: "one", Host: "test.invalid", Port: 22, User: "operator", AuthType: "password", Password: "old-password", SSHOptions: models.SSHOptions{SudoAutoFill: true}}
	latest := connected
	latest.Password = "new-password"
	if err := st.Save(latest); err != nil {
		t.Fatal(err)
	}
	if password, err := a.savedSudoPassword(connected); err != nil || password != "new-password" {
		t.Fatal("did not use latest saved password", err)
	}
	for _, mutation := range []string{"host", "port", "user", "disabled", "privateKey", "agent", "empty"} {
		t.Run(mutation, func(t *testing.T) {
			node := latest
			switch mutation {
			case "host":
				node.Host = "other.invalid"
			case "port":
				node.Port = 2222
			case "user":
				node.User = "other"
			case "disabled":
				node.SudoAutoFill = false
			case "privateKey", "agent":
				node.AuthType = mutation
			case "empty":
				node.Password = ""
			}
			if err := st.Save(node); err != nil {
				t.Fatal(err)
			}
			if password, err := a.savedSudoPassword(connected); err == nil || password != "" {
				t.Fatal("wrong credential returned")
			}
		})
	}
	if err := st.Delete(connected.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.savedSudoPassword(connected); err == nil {
		t.Fatal("deleted server used stale password")
	}
}
