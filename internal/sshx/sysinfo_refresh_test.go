package sshx

import "testing"

func TestRunningCollectorRequestsFreshSnapshot(t *testing.T) {
	c := newSysInfoCollector("test", nil, nil, nil)
	c.running = true
	for i := 0; i < 3; i++ {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.refresh) != 1 {
		t.Fatalf("refresh requests must be queued and coalesced: %d", len(c.refresh))
	}
	<-c.refresh
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if len(c.refresh) != 1 {
		t.Fatal("reopening must request another sample")
	}
}
