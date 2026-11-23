package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/models"
)

func TestHistoryLifecycleFilteringAndLimits(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewSQLiteHistoryStore(db)
	for _, command := range []string{"", "\x1b[A", "[A", "mysql -psecret", "PASSWORD=secret tool", "NAME STATUS", "warning: output"} {
		if err := h.Add("a", command); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := h.Query("a", "", 10); len(got) != 0 {
		t.Fatal("noise or secret command retained", got)
	}
	for _, command := range []string{"  git   status  ", "git status", "echo '中文é'", "echo '100%'", "echo 'a_b'", `echo 'a\b'`} {
		if err := h.Add("a", command); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.Query("a", "git", 8)
	if err != nil || len(got) != 1 || got[0].Command != "git status" || got[0].Count != 1 {
		t.Fatal(got, err)
	}
	for _, prefix := range []string{"100%", "a_b", `a\b`, "中文é"} {
		got, err := h.Query("a", prefix, 8)
		if err != nil || len(got) != 1 {
			t.Fatalf("literal search %q: %+v %v", prefix, got, err)
		}
	}
	if err := h.Add("b", "other-server"); err != nil {
		t.Fatal(err)
	}
	if err := h.Clear("a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.Query("a", "", 8); len(got) != 0 {
		t.Fatal(got)
	}
	if got, _ := h.Query("b", "", 8); len(got) != 1 {
		t.Fatal("clear affected another server")
	}
	// Seed the boundary in one transaction, then exercise production eviction.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < historyMaxPerServer; i++ {
		if _, err := tx.Exec("INSERT INTO command_history(server_id,command,executed_at) VALUES ('a',?,?)", fmt.Sprint("echo ", i), i); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := h.Add("a", "last-command"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM command_history WHERE server_id='a'").Scan(&count); err != nil || count != historyMaxPerServer {
		t.Fatal(count, err)
	}
	if err := h.Clear(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.Query("b", "", 0); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestDingpackEncryptedRoundTripAndRejection(t *testing.T) {
	servers := []models.ServerNode{{ID: "audit", Name: "中文é", Host: "localhost", Password: "synthetic-secret"}}
	creds := []models.Credential{{ID: "credential", Name: "fixture", Password: "credential-secret"}}
	groups := []string{"project/生产"}
	blob, err := BuildDingpack("audit-pass", servers, creds, groups)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("synthetic-secret")) {
		t.Fatal("plaintext leaked into export")
	}
	got, err := ParseDingpack("audit-pass", blob)
	if err != nil || !reflect.DeepEqual(got.Servers, servers) || !reflect.DeepEqual(got.Credentials, creds) || !reflect.DeepEqual(got.Groups, groups) {
		t.Fatal("payload roundtrip", err)
	}
	if _, err := ParseDingpack("wrong", blob); err == nil {
		t.Fatal("wrong password accepted")
	}
	blob[len(blob)-1] ^= 1
	if _, err := ParseDingpack("audit-pass", blob); err == nil {
		t.Fatal("tampered payload accepted")
	}
	if _, err := BuildDingpack("", nil, nil, nil); err == nil {
		t.Fatal("empty password accepted")
	}
	if _, err := BuildDingpack("pass", make([]models.ServerNode, 501), nil, nil); err == nil {
		t.Fatal("export limit bypassed")
	}
	if _, err := ParseDingpack("pass", []byte{1, 2, 3}); err == nil {
		t.Fatal("truncated payload accepted")
	}
	path := filepath.Join(t.TempDir(), "audit.dingpack")
	if err := WriteDingpackFile(path, "pass", servers, creds, groups); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDingpackFile(path, "pass"); err != nil {
		t.Fatal(err)
	}
}

func TestDingpackImportValidatesVersionAndServerLimit(t *testing.T) {
	for _, payload := range []DingpackPayload{
		{Magic: dingpackMagic, Version: 999},
		{Magic: dingpackMagic, Version: 1, Servers: make([]models.ServerNode, 501)},
	} {
		t.Run(fmt.Sprintf("version-%d-servers-%d", payload.Version, len(payload.Servers)), func(t *testing.T) {
			raw, _ := json.Marshal(payload)
			blob, err := cryptox.SealPayload("pass", raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseDingpack("pass", blob); err == nil {
				t.Fatalf("unsupported export accepted: version=%d servers=%d", payload.Version, len(payload.Servers))
			}
		})
	}
}
