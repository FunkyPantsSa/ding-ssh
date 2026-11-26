package apphost

import (
	"testing"

	"ding-ssh/internal/models"
)

type recordedSessionBadge struct {
	label   string
	sets    int
	removes int
}

func (b *recordedSessionBadge) SetBadge(label string) error {
	b.label = label
	b.sets++
	return nil
}

func (b *recordedSessionBadge) RemoveBadge() error {
	b.label = ""
	b.removes++
	return nil
}

func TestWindowsSessionsNeverCreateNotificationBadge(t *testing.T) {
	badge := &recordedSessionBadge{label: "1"}
	for _, status := range []models.SessionStatus{models.StatusConnected, models.StatusConnecting, models.StatusClosed, models.StatusError} {
		if err := syncSessionBadge(badge, "windows", []models.SessionInfo{{Status: status}}); err != nil {
			t.Fatal(err)
		}
		if badge.label != "" || badge.sets != 0 {
			t.Fatalf("session status %s left a notification badge: %+v", status, badge)
		}
	}
	if badge.removes != 4 {
		t.Fatalf("expected stale badges to be cleared for every session update, got %d", badge.removes)
	}
}

func TestDockBadgeCountsOnlyConnectedSessions(t *testing.T) {
	badge := &recordedSessionBadge{}
	sessions := []models.SessionInfo{
		{Status: models.StatusConnected},
		{Status: models.StatusConnected},
		{Status: models.StatusClosed},
		{Status: models.StatusError},
	}
	if err := syncSessionBadge(badge, "darwin", sessions); err != nil {
		t.Fatal(err)
	}
	if badge.label != "2" {
		t.Fatalf("expected two active sessions, got %q", badge.label)
	}
	if err := syncSessionBadge(badge, "darwin", sessions[2:]); err != nil {
		t.Fatal(err)
	}
	if badge.label != "" || badge.removes != 1 {
		t.Fatalf("closed sessions left a stale badge: %+v", badge)
	}
}
