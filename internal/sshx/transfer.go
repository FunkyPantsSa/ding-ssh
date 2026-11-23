package sshx

import (
	"context"
	"io"
	"runtime"
	"sync"
	"time"

	"ding-ssh/internal/logx"

	"golang.org/x/time/rate"
)

// defaultMaxConcurrent 默认最大并发传输数：CPU 核心数 * 2
var defaultMaxConcurrent = runtime.NumCPU() * 2

// defaultRateLimit 默认速率限制：0 表示不限制
var defaultRateLimit = rate.Limit(0)

// defaultBurst 默认突发大小
var defaultBurst = 64 * 1024 // 64KB

// transferProgressLog 传输进度采样器：只用于热路径每块/每条进度的 DEBUG 限流。
var transferProgressLog = logx.NewSampler(0, 2*time.Second)

// TransferLimiter 传输限速器，基于令牌桶实现全局与单任务限速。
type TransferLimiter struct {
	mu       sync.RWMutex
	global   *rate.Limiter
	perTask  *rate.Limiter
	enabled  bool
	bytesSec int64 // 字节/秒，0 表示不限速
}

// NewTransferLimiter 创建传输限速器，默认不限速。
func NewTransferLimiter() *TransferLimiter {
	return &TransferLimiter{
		global:   rate.NewLimiter(rate.Inf, defaultBurst),
		perTask:  rate.NewLimiter(rate.Inf, defaultBurst),
		enabled:  false,
		bytesSec: 0,
	}
}

// SetRateLimit 设置全局速率限制（字节/秒），0 表示不限速。
func (tl *TransferLimiter) SetRateLimit(bytesPerSec int64) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.bytesSec = bytesPerSec
	if bytesPerSec <= 0 {
		tl.global = rate.NewLimiter(rate.Inf, defaultBurst)
		tl.enabled = false
		logx.Infof("[sshx] 传输限速已关闭: bytesPerSec=0")
	} else {
		burst := int(max(1, min(int64(defaultBurst), bytesPerSec/4)))
		// Keep each snapshot stable while a transfer is waiting. Reconfiguration
		// must not shrink its burst between choosing a chunk and WaitN.
		tl.global = rate.NewLimiter(rate.Limit(bytesPerSec), burst)
		tl.enabled = true
		logx.Infof("[sshx] 传输限速已设置: bytesPerSec=%d burst=%d", bytesPerSec, burst)
	}
}

// Wait 等待限速器允许通过 N 字节，阻塞直到令牌可用。
func (tl *TransferLimiter) Wait(ctx context.Context, n int) error {
	tl.mu.RLock()
	enabled, limiter := tl.enabled, tl.global
	tl.mu.RUnlock()
	if !enabled {
		// The caller may already have read a final partial chunk when the
		// context was cancelled. Preserve those bytes; its loop handles cancel.
		return nil
	}
	for n > 0 {
		chunk := min(n, limiter.Burst())
		if err := limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

// TransferWorkerPool 传输 Worker 池，控制并发传输数。
type TransferWorkerPool struct {
	limiter *TransferLimiter
	mu      sync.Mutex
	active  int
	limit   int
	changed chan struct{}
}

// NewTransferWorkerPool 创建传输 Worker 池。
func NewTransferWorkerPool() *TransferWorkerPool {
	return &TransferWorkerPool{
		limiter: NewTransferLimiter(),
		limit:   defaultMaxConcurrent,
		changed: make(chan struct{}),
	}
}

// Acquire 获取一个 Worker 槽位，阻塞直到有空闲。
func (p *TransferWorkerPool) Acquire(ctx context.Context) error {
	for {
		p.mu.Lock()
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return err
		}
		if p.active < p.limit {
			p.active++
			p.mu.Unlock()
			return nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release 释放一个 Worker 槽位。
func (p *TransferWorkerPool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active > 0 {
		p.active--
		close(p.changed)
		p.changed = make(chan struct{})
	}
}

// Limiter 返回限速器。
func (p *TransferWorkerPool) Limiter() *TransferLimiter {
	return p.limiter
}

// SetConcurrency 设置最大并发数。
func (p *TransferWorkerPool) SetConcurrency(n int) {
	if n < 1 {
		logx.Warnf("[sshx] 传输并发数非法，回落为 1: requested=%d", n)
		n = 1
	}
	logx.Infof("[sshx] 传输并发数已更新: concurrency=%d", n)
	p.mu.Lock()
	p.limit = n
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

// RateLimitedReader 带限速的读取器，在每次读取后等待限速器。
type RateLimitedReader struct {
	reader  io.Reader
	limiter *TransferLimiter
	ctx     context.Context
}

// NewRateLimitedReader 创建带限速的读取器。
func NewRateLimitedReader(ctx context.Context, reader io.Reader, limiter *TransferLimiter) *RateLimitedReader {
	return &RateLimitedReader{
		reader:  reader,
		limiter: limiter,
		ctx:     ctx,
	}
}

// Read 实现 io.Reader，每次读取后等待限速器。
func (r *RateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		if waitErr := r.limiter.Wait(r.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

// DefaultTransferPool 默认全局传输 Worker 池。
var DefaultTransferPool = NewTransferWorkerPool()

// ThrottledCopy 带限速和并发控制的流式拷贝。
func ThrottledCopy(ctx context.Context, dst io.Writer, src io.Reader, pool *TransferWorkerPool) (int64, error) {
	if pool == nil {
		pool = DefaultTransferPool
	}
	if err := pool.Acquire(ctx); err != nil {
		return 0, err
	}
	defer pool.Release()

	limitedSrc := NewRateLimitedReader(ctx, src, pool.Limiter())
	buf := make([]byte, 64*1024)
	var total int64
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, rerr := limitedSrc.Read(buf)
		if n > 0 {
			wn, werr := dst.Write(buf[:n])
			total += int64(wn)
			if werr != nil {
				return total, werr
			}
			if wn != n {
				return total, io.ErrShortWrite
			}
			logx.DebugSampled(transferProgressLog, "[sshx] 限速拷贝进度: bytes=%d", total)
		}
		if rerr == io.EOF {
			logx.Infof("[sshx] 限速拷贝完成: bytes=%d elapsed=%s", total, time.Since(start))
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// ProgressWriter 进度上报写入器，每 100ms 限频上报。
type ProgressWriter struct {
	writer      io.Writer
	total       int64
	transferred int64
	onProgress  func(transferred int64, total int64)
	lastReport  time.Time
}

// NewProgressWriter 创建进度上报写入器。
func NewProgressWriter(w io.Writer, total int64, onProgress func(transferred, total int64)) *ProgressWriter {
	return &ProgressWriter{
		writer:     w,
		total:      total,
		onProgress: onProgress,
		lastReport: time.Now(),
	}
}

// Write 实现 io.Writer，写入后上报进度（限频 100ms）。
func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 {
		pw.transferred += int64(n)
		if pw.onProgress != nil && time.Since(pw.lastReport) > 100*time.Millisecond {
			pw.onProgress(pw.transferred, pw.total)
			pw.lastReport = time.Now()
			// 只记累计字节与百分比，不记任何内容。
			if pw.total > 0 {
				logx.DebugSampled(transferProgressLog, "[sshx] 写出进度: transferred=%d total=%d pct=%d%%",
					pw.transferred, pw.total, pw.transferred*100/pw.total)
			}
		}
	}
	return n, err
}
