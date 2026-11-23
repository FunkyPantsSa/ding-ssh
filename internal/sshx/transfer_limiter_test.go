package sshx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransferLimiterAcceptsChunksLargerThanBurst(t *testing.T) {
	limiter := NewTransferLimiter()
	limiter.SetRateLimit(128 * 1024)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := limiter.Wait(ctx, 64*1024); err != nil {
		t.Fatalf("a normal 64 KiB transfer chunk must not exceed the limiter burst: %v", err)
	}
}

type partialAuditWriter struct{ err error }

func (w partialAuditWriter) Write(p []byte) (int, error) { return min(2, len(p)), w.err }

func TestFolderCopyCountsPartialWritesAndRejectsShortWrites(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		var transferred atomic.Int64
		err := copyFolderChunk(context.Background(), bytes.NewReader([]byte("data")), partialAuditWriter{writeErr}, &transferred)
		want := writeErr
		if want == nil {
			want = io.ErrShortWrite
		}
		if !errors.Is(err, want) || transferred.Load() != 2 {
			t.Fatalf("partial folder write bytes=%d err=%v", transferred.Load(), err)
		}
	}
}

func TestTransferLimiterSmallRatesHaveNonzeroBurst(t *testing.T) {
	limiter := NewTransferLimiter()
	limiter.SetRateLimit(3)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := limiter.Wait(ctx, 1); err != nil {
		t.Fatalf("positive rates must allow at least one byte: %v", err)
	}
}

func TestTransferLimiterConcurrentReconfiguration(t *testing.T) {
	limiter := NewTransferLimiter()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			limiter.SetRateLimit(int64(i%2) * 1024 * 1024)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = limiter.Wait(context.Background(), 1)
		}
	}()
	wg.Wait()
}

func TestTransferPoolResizePreservesActiveWorkers(t *testing.T) {
	pool := NewTransferWorkerPool()
	pool.SetConcurrency(1)
	if err := pool.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	pool.SetConcurrency(2)
	done := make(chan struct{})
	go func() { pool.Release(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resizing discarded the active worker's release token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pool.Acquire(ctx); err == nil {
		pool.Release()
		t.Fatal("cancelled acquisition succeeded")
	}
}

func TestThrottledCopyRoundTripAndCancellation(t *testing.T) {
	payload := bytes.Repeat([]byte("audit-中文\x00"), 12000)
	var out bytes.Buffer
	n, err := ThrottledCopy(context.Background(), &out, bytes.NewReader(payload), NewTransferWorkerPool())
	if err != nil || n != int64(len(payload)) || !bytes.Equal(payload, out.Bytes()) {
		t.Fatalf("roundtrip bytes=%d err=%v", n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ThrottledCopy(ctx, &out, bytes.NewReader(payload), NewTransferWorkerPool()); err == nil {
		t.Fatal("cancelled copy succeeded")
	}
}

func TestTransferPoolShrinkWaitsForActiveWorkers(t *testing.T) {
	p := NewTransferWorkerPool()
	p.SetConcurrency(2)
	for i := 0; i < 2; i++ {
		if err := p.Acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	p.SetConcurrency(1)
	p.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx); err == nil {
		t.Fatal("shrinking admitted more than the new limit")
	}
	p.Release()
	if err := p.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.Release()
}
