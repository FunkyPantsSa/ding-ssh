package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ding-ssh/internal/logx"
)

// BridgeRequestEvent 后端 → 前端的请求事件名。
// 前端监听它，执行后通过 Wails 绑定 DebugReply(id, payload) 回包。
const BridgeRequestEvent = "debug:request"

// Bridge 把 Go 侧的请求投递到前端（WebView）并等待回包。
// 用于访问只有前端才知道的状态：xterm 实例（rows/cols、缓冲区、滚动位置）、页面 DOM 几何等。
type Bridge struct {
	emit    func(event string, payload any)
	timeout time.Duration

	mu          sync.Mutex
	seq         uint64
	pending     map[string]chan bridgeReply
	lastSuccess int64
	lastFailure int64
	lastRTTMs   int64
	failures    uint64
}

type bridgeReply struct {
	payload json.RawMessage
	err     error
}

// NewBridge 创建前端桥。emit 通常是 wailsruntime.EventsEmit 的封装。
func NewBridge(emit func(event string, payload any)) *Bridge {
	logx.Debugf("[debugsrv/bridge] 创建前端桥: timeout=%s", 10*time.Second)
	return &Bridge{
		emit:    emit,
		timeout: 10 * time.Second,
		pending: make(map[string]chan bridgeReply),
	}
}

// SetTimeout 调整等待前端回包的超时时间。
func (b *Bridge) SetTimeout(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d > 0 {
		logx.Debugf("[debugsrv/bridge] 调整前端桥超时: from=%s to=%s", b.timeout, d)
		b.timeout = d
	}
}

// Stats is independent of the WebView, so a blocked JS thread cannot hide
// missing replies. A past successful reply is never reported as current ready.
func (b *Bridge) Stats() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := "unknown"
	if b.lastSuccess > 0 {
		state = "healthy"
	}
	if b.lastFailure > b.lastSuccess {
		state = "degraded"
	}
	return map[string]any{"state": state, "pending": len(b.pending), "lastSuccessAt": b.lastSuccess, "lastFailureAt": b.lastFailure, "lastRTTMs": b.lastRTTMs, "failureCount": b.failures}
}

// Call 请求前端执行 op，返回前端给的 JSON 结果。
func (b *Bridge) Call(ctx context.Context, op string, args any) (json.RawMessage, error) {
	if b.emit == nil {
		logx.Warnf("[debugsrv/bridge] 前端桥未初始化，无法投递请求: op=%s", op)
		return nil, &OperationError{Code: "FRONTEND_NOT_READY", Message: "前端桥未初始化", Retryable: true, RetryAfterMs: 250, NextSteps: []string{"调用 app.health 检查 readiness.frontendResponsive，等待界面初始化后重试"}}
	}
	id := fmt.Sprintf("dbg-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&b.seq, 1))
	ch := make(chan bridgeReply, 1)

	b.mu.Lock()
	b.pending[id] = ch
	timeout := b.timeout
	b.mu.Unlock()
	// Observation actions can carry an explicit postcondition wait budget up to
	// 15 seconds. Let a caller-owned context extend the historical 10-second
	// bridge default, while keeping an absolute cap for callers that forget a
	// deadline. The context remains the authoritative cancellation signal.
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > timeout {
			timeout = remaining
		}
		if timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
	}
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	payload, err := json.Marshal(map[string]any{"id": id, "op": op, "args": args})
	if err != nil {
		logx.Warnf("[debugsrv/bridge] 序列化前端请求失败: op=%s id=%s err=%v", op, id, err)
		return nil, fmt.Errorf("序列化前端请求失败: %w", err)
	}
	logx.Debugf("[debugsrv/bridge] 投递前端请求: op=%s id=%s bytes=%d", op, id, len(payload))
	b.emit(BridgeRequestEvent, json.RawMessage(payload))

	start := time.Now()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case rep := <-ch:
		b.mu.Lock()
		b.lastRTTMs = time.Since(start).Milliseconds()
		if rep.err == nil {
			b.lastSuccess = time.Now().UnixMilli()
		} else {
			b.lastFailure = time.Now().UnixMilli()
			b.failures++
		}
		b.mu.Unlock()
		if rep.err != nil {
			logx.Debugf("[debugsrv/bridge] 前端请求失败: op=%s elapsed=%s err=%v", op, time.Since(start), rep.err)
			return rep.payload, rep.err
		}
		logx.Debugf("[debugsrv/bridge] 前端请求完成: op=%s elapsed=%s bytes=%d", op, time.Since(start), len(rep.payload))
		return rep.payload, rep.err
	case <-timer.C:
		b.mu.Lock()
		b.lastFailure = time.Now().UnixMilli()
		b.failures++
		b.mu.Unlock()
		logx.Warnf("[debugsrv/bridge] 前端响应超时: op=%s timeout=%s elapsed=%s", op, timeout, time.Since(start))
		return nil, &OperationError{Code: "FRONTEND_TIMEOUT", Message: fmt.Sprintf("前端响应超时（op=%s）", op), Cause: context.DeadlineExceeded, OutcomeUnknown: true, NextSteps: []string{"调用 app.health 检查前端状态，并先查询操作结果；前端操作可能已执行，请勿直接重复写操作"}}
	case <-ctx.Done():
		logx.Debugf("[debugsrv/bridge] 前端请求被取消: op=%s elapsed=%s err=%v", op, time.Since(start), ctx.Err())
		return nil, ctx.Err()
	}
}

// CallJSON 同 Call，但把结果反序列化到 out。
func (b *Bridge) CallJSON(ctx context.Context, op string, args any, out any) error {
	raw, err := b.Call(ctx, op, args)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Reply 由 Wails 绑定在前端回包时调用。
// payload 形如 {"ok":true,"data":...} 或 {"ok":false,"error":"..."}。
func (b *Bridge) Reply(id string, payload string) {
	b.mu.Lock()
	ch := b.pending[id]
	b.mu.Unlock()
	if ch == nil {
		// 迟到的回包（请求已超时 / 已取消）：只留痕迹，不影响任何调用方。
		logx.Debugf("[debugsrv/bridge] 收到未知请求的前端回包，已丢弃: id=%s bytes=%d", id, len(payload))
		return
	}
	var envelope struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		logx.Warnf("[debugsrv/bridge] 前端回包不是合法 JSON: id=%s bytes=%d err=%v", id, len(payload), err)
		ch <- bridgeReply{err: fmt.Errorf("前端回包不是合法 JSON: %w", err)}
		return
	}
	if !envelope.OK {
		msg := envelope.Error
		if msg == "" {
			msg = "前端执行失败"
		}
		logx.Warnf("[debugsrv/bridge] 前端执行失败: id=%s reason=%s", id, msg)
		ch <- bridgeReply{err: errors.New(msg)}
		return
	}
	logx.Debugf("[debugsrv/bridge] 前端回包成功: id=%s bytes=%d", id, len(envelope.Data))
	ch <- bridgeReply{payload: envelope.Data}
}
