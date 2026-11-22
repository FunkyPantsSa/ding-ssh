package debugsrv

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var uiSnapshotMu sync.Mutex

// Named snapshots are private, ephemeral diagnostic data. Retain up to 64
// snapshot pairs and 64 MiB, and remove files older than 24 hours on capture.
func pruneUISnapshots(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type snapshot struct {
		name     string
		files    []string
		bytes    int64
		modified time.Time
	}
	groups := map[string]*snapshot{}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext != ".png" && ext != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ext)
		if _, err := validateUISnapshotName(name); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		g := groups[name]
		if g == nil {
			g = &snapshot{name: name}
			groups[name] = g
		}
		g.files = append(g.files, filepath.Join(dir, entry.Name()))
		g.bytes += info.Size()
		if info.ModTime().After(g.modified) {
			g.modified = info.ModTime()
		}
	}
	all := make([]*snapshot, 0, len(groups))
	var total int64
	for _, g := range groups {
		all = append(all, g)
		total += g.bytes
	}
	sort.Slice(all, func(i, j int) bool { return all[i].modified.Before(all[j].modified) })
	remaining := len(all)
	for _, g := range all {
		if now.Sub(g.modified) <= 24*time.Hour && remaining <= 64 && total <= 64<<20 {
			continue
		}
		for _, file := range g.files {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		total -= g.bytes
		remaining--
	}
	return nil
}
