package sshx

import (
	"testing"

	"ding-ssh/internal/models"
)

func TestSFTPCacheIsScopedBySession(t *testing.T) {
	cache := NewSFTPCacheManager()
	cache.SetForSession("server-a", "/home/user", []models.SFTPEntry{{Name: "from-a.txt"}})
	cache.SetForSession("server-b", "/home/user", []models.SFTPEntry{{Name: "from-b.txt"}})

	for session, want := range map[string]string{"server-a": "from-a.txt", "server-b": "from-b.txt"} {
		node, ok := cache.GetForSession(session, "/home/user")
		if !ok || len(node.Entries) != 1 || node.Entries[0].Name != want {
			t.Fatalf("cache for %s = %#v, %v; want %q", session, node, ok, want)
		}
	}

	cache.InvalidateForSession("server-a", "/home/user")
	if _, ok := cache.GetForSession("server-a", "/home/user"); ok {
		t.Fatal("invalidated session should not retain the directory")
	}
	if node, ok := cache.GetForSession("server-b", "/home/user"); !ok || node.Entries[0].Name != "from-b.txt" {
		t.Fatalf("invalidating server-a must not affect server-b: node=%#v ok=%v", node, ok)
	}
}

func TestSFTPCacheInvalidationRejectsInFlightRefresh(t *testing.T) {
	cache := NewSFTPCacheManager()
	cache.SetForSession("server-a", "/home/user", []models.SFTPEntry{{Name: "old.txt"}})
	cache.SetForSession("server-a", "/var/log", []models.SFTPEntry{{Name: "system.log"}})
	started := make(chan struct{})
	continueRead := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := cache.RevalidateForSession("server-a", "/home/user", func(string) ([]models.SFTPEntry, error) {
			close(started)
			<-continueRead
			return []models.SFTPEntry{{Name: "new.txt"}}, nil
		})
		result <- err
	}()
	<-started
	cache.InvalidateForSession("server-a", "/home/user")
	close(continueRead)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.GetForSession("server-a", "/home/user"); ok {
		t.Fatal("an in-flight refresh must not restore entries after invalidation")
	}
	if node, ok := cache.GetForSession("server-a", "/var/log"); !ok || node.Entries[0].Name != "system.log" {
		t.Fatalf("invalidating one directory should preserve other cached paths: node=%#v ok=%v", node, ok)
	}
}

func TestSFTPCacheGlobalInvalidationRejectsInitialRead(t *testing.T) {
	cache := NewSFTPCacheManager()
	key := cache.keyForSession("server-a", "/home/user")
	cache.Invalidate("/home/user") // no cached entry exists yet to delete
	if cache.storeByKey(key, &DirCacheNode{Path: "/home/user/", Entries: []models.SFTPEntry{{Name: "stale.txt"}}}) {
		t.Fatal("a listing started before global invalidation must not repopulate the cache")
	}
	if _, ok := cache.GetForSession("server-a", "/home/user"); ok {
		t.Fatal("initial read invalidated while in flight must not populate the cache")
	}
}
