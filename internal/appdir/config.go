// Package appdir centralizes the application's writable data directory.
package appdir

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvDevConfigDir is an explicit development/test-only override. The value
// must be an absolute path so a typo cannot redirect writes into the repo.
const EnvDevConfigDir = "DING_SSH_DEV_CONFIG_DIR"

// ConfigDir returns the directory containing the database, settings, keys,
// logs and debug discovery file.
func ConfigDir() string {
	if override := strings.TrimSpace(os.Getenv(EnvDevConfigDir)); filepath.IsAbs(override) {
		return filepath.Clean(override)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(dir, "ding-ssh")
}

// IsDevOverride reports whether an absolute development override is active.
func IsDevOverride() bool {
	return filepath.IsAbs(strings.TrimSpace(os.Getenv(EnvDevConfigDir)))
}
