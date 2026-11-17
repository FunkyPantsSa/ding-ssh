package apphost

import (
	"ding-ssh/internal/updater"
	"testing"
)

func TestUpdateUsesExplicitBuildCommit(t *testing.T) {
	old := updater.BuildCommit
	t.Cleanup(func() { updater.BuildCommit = old })
	updater.BuildCommit = "91cab483a098436703ae1d6ff6727254b90c669b"
	if got := currentUpdateCommit(); got != updater.BuildCommit {
		t.Fatalf("current commit = %q, want injected commit %q", got, updater.BuildCommit)
	}
}
