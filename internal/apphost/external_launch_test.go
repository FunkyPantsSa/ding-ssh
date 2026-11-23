package apphost

import (
	"sync"
	"testing"
)

type mockEmitter struct {
	mu     sync.Mutex
	events []struct {
		name string
		data []any
	}
}

func TestHandleExternalLaunch(t *testing.T) {
	app := &App{}
	var capturedEvents []string
	var capturedData []any
	var mu sync.Mutex

	// We can hook a test desktop event or verify string detection
	args := []string{
		"ssh://user@example.com:2222",
		"ding-ssh://connect/server-123",
		"/Users/test/backup.dingpack",
		"--ignored-flag",
	}

	app.desktop = nil // avoid nil panic if emit checks a.desktop != nil
	// Let's test emit logic or mock desktop
	mu.Lock()
	defer mu.Unlock()

	// Verify HandleExternalLaunch runs without panics
	app.HandleExternalLaunch(args)
	_ = capturedEvents
	_ = capturedData
}

func TestDisconnectAll(t *testing.T) {
	app := &App{}
	// Should not panic on nil managers
	app.DisconnectAll()
}
