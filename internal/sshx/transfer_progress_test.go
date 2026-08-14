package sshx

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/models"
)

// TestStreamTransferEmitsProgressDuringCopy 确认进度事件在拷贝尚未返回时就能发出，
// 而不是等 SftpUpload 绑定调用结束后才一次性刷出。
func TestStreamTransferEmitsProgressDuringCopy(t *testing.T) {
	var mu sync.Mutex
	var events []models.SFTPTransferEvent
	progressSeen := make(chan struct{})
	m := NewManager(func(_ string, payload interface{}) {
		evt, ok := payload.(models.SFTPTransferEvent)
		if !ok {
			return
		}
		mu.Lock()
		events = append(events, evt)
		n := len(events)
		mu.Unlock()
		if n == 1 && !evt.Done {
			close(progressSeen)
		}
	})

	payload := bytes.Repeat([]byte("x"), 128*1024)
	src := &gateReader{r: bytes.NewReader(payload), gate: progressSeen}
	var dst bytes.Buffer

	err := m.streamTransfer(context.Background(), "sess-1", "upload", "big.bin", int64(len(payload)), src.Read, dst.Write)
	if err != nil {
		t.Fatalf("streamTransfer: %v", err)
	}
	if dst.Len() != len(payload) {
		t.Fatalf("wrote %d, want %d", dst.Len(), len(payload))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least start + done", len(events))
	}
	if events[0].Done {
		t.Fatal("first event should be in-progress")
	}
	last := events[len(events)-1]
	if !last.Done {
		t.Fatal("last event should be done")
	}
	if last.Transferred != int64(len(payload)) {
		t.Fatalf("final transferred=%d, want %d", last.Transferred, len(payload))
	}
	if last.Name != "big.bin" || last.Direction != "upload" {
		t.Fatalf("unexpected last event: %+v", last)
	}
}

type gateReader struct {
	r    io.Reader
	gate <-chan struct{}
	once sync.Once
}

func (g *gateReader) Read(p []byte) (int, error) {
	g.once.Do(func() {
		select {
		case <-g.gate:
		case <-time.After(2 * time.Second):
		}
	})
	return g.r.Read(p)
}
