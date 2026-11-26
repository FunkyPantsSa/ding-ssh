package apphost

import (
	"strconv"

	"ding-ssh/internal/models"
)

type sessionBadge interface {
	SetBadge(string) error
	RemoveBadge() error
}

func syncSessionBadge(badge sessionBadge, platform string, sessions []models.SessionInfo) error {
	// Windows renders this as a notification overlay. Session counts are already
	// visible in the UI and tray menu, so they must not create a persistent alert.
	if platform == "windows" {
		return badge.RemoveBadge()
	}
	count := 0
	for _, session := range sessions {
		if session.Status == models.StatusConnected {
			count++
		}
	}
	if count == 0 {
		return badge.RemoveBadge()
	}
	return badge.SetBadge(strconv.Itoa(count))
}
