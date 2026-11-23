package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func TestSameNameTransfersHaveDistinctIDsAndExactCancellation(t *testing.T) {
	m := NewManager(func(string, interface{}) {})
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	a := m.registerTransfer("s", "upload", "same.txt", cancelA)
	defer m.unregisterTransfer(a)
	b := m.registerTransfer("s", "upload", "same.txt", cancelB)
	defer m.unregisterTransfer(b)
	if a.id == "" || a.id == b.id {
		t.Fatal("task IDs must be distinct")
	}
	if err := m.SftpCancelTransfer("s", "upload", "same.txt"); err == nil {
		t.Fatal("ambiguous legacy cancellation must fail")
	}
	if ctxA.Err() != nil || ctxB.Err() != nil {
		t.Fatal("ambiguous cancellation stopped a task")
	}
	if err := m.SftpCancelTransfer("s", "upload", "@id:"+a.id); err != nil {
		t.Fatal(err)
	}
	if ctxA.Err() == nil || ctxB.Err() != nil {
		t.Fatal("exact cancellation affected wrong task")
	}
}

func TestTransferProgressKeepsOneIDAcrossStartAndFinish(t *testing.T) {
	var ids []string
	m := NewManager(func(_ string, p interface{}) {
		if e, ok := p.(models.SFTPTransferEvent); ok {
			ids = append(ids, e.TransferID)
		}
	})
	var dst bytes.Buffer
	for i := 0; i < 2; i++ {
		ids = nil
		src := bytes.NewReader([]byte("data"))
		if err := m.streamTransfer(context.Background(), "s", "upload", "same", 4, src.Read, dst.Write); err != nil {
			t.Fatal(err)
		}
		if len(ids) < 2 || ids[0] == "" {
			t.Fatalf("missing ID: %v", ids)
		}
		for _, id := range ids {
			if id != ids[0] {
				t.Fatalf("ID changed within task: %v", ids)
			}
		}
	}
}

func TestCancelledStreamReturnsCancellationWithPartialBytes(t *testing.T) {
	var final models.SFTPTransferEvent
	m := NewManager(func(_ string, p interface{}) {
		if e, ok := p.(models.SFTPTransferEvent); ok && e.Done {
			final = e
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dst bytes.Buffer
	read := func(b []byte) (int, error) { b[0] = 'x'; cancel(); return 1, nil }
	err := m.streamTransfer(ctx, "s", "upload", "partial", 100, read, dst.Write)
	if !errors.Is(err, ErrTransferCancelled) {
		t.Fatalf("cancelled transfer reported success: %v", err)
	}
	if final.TransferID == "" || final.Error != "已取消" || final.Transferred != 1 || final.Total != 100 {
		t.Fatalf("incorrect cancellation progress: %+v", final)
	}
}

func TestStreamTransferCountsPartialWritesAndRejectsShortWrites(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		t.Run(fmt.Sprint(writeErr), func(t *testing.T) {
			var final models.SFTPTransferEvent
			m := NewManager(func(_ string, p interface{}) {
				if e, ok := p.(models.SFTPTransferEvent); ok && e.Done {
					final = e
				}
			})
			src := bytes.NewReader([]byte("data"))
			err := m.streamTransfer(context.Background(), "s", "upload", "partial", 4, src.Read, func([]byte) (int, error) { return 2, writeErr })
			want := writeErr
			if want == nil {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) {
				t.Fatalf("partial write reported wrong result: %v", err)
			}
			if final.Transferred != 2 || final.Error == "" {
				t.Fatalf("partial write lost progress or error: %+v", final)
			}
		})
	}
}
