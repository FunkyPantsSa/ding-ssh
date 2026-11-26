package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"ding-ssh/internal/models"
)

func TestSQLiteBusyTimeoutOnEveryConnection(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "设置 data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Keep connections checked out so the pool must open additional ones.
	for i := 0; i < 6; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var timeout int
		if err := conn.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 {
			t.Errorf("connection %d busy_timeout = %d, want 5000", i, timeout)
		}
	}
}

func TestSQLiteSettingsSaveRollsBackAllKeys(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewSQLiteSettingsStore(db)
	st := models.Settings{UIScale: 100, LocalShell: "original"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	// Fail a late key after uiScale has already been written.
	if _, err := db.Exec(`CREATE TRIGGER reject_setting BEFORE UPDATE ON settings
		WHEN NEW.key = 'localShell' AND NEW.value = 'reject'
		BEGIN SELECT RAISE(ABORT, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	st.UIScale, st.LocalShell = 125, "reject"
	if err := s.Save(st); err == nil {
		t.Fatal("expected save failure")
	}
	got, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.UIScale != 100 || got.LocalShell != "original" {
		t.Fatalf("failed save left partially updated settings: scale=%d shell=%q", got.UIScale, got.LocalShell)
	}
}

func TestSQLiteConcurrentSettingsSavesStayConsistent(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewSQLiteSettingsStore(db)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := s.Save(models.Settings{UIScale: 100 + i, LocalShell: fmt.Sprint(100 + i)}); err != nil {
				t.Errorf("concurrent save %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	got, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalShell != fmt.Sprint(got.UIScale) {
		t.Fatalf("settings mixed across saves: scale=%d shell=%q", got.UIScale, got.LocalShell)
	}
}
