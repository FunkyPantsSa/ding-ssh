package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BridgeRequestEvent 后端 → 前端的请求事件名。
// 前端监听它，执行后通过 Wails 绑定 DebugReply(id, payload) 回包。
const BridgeRequestEvent = "debug:request"

// Bridge 把 Go 侧的请求投递到前端（WebView）并等待回包。
// 用于访问只有前端才知道的状态：xterm 实例（rows/cols、缓冲区、滚动位置）、页面 DOM 几何等。
type Bridge struct {
	emit    func(event string, payload any)
	timeout time.Duration

	mu      sync.Mutex
	seq     uint64
	pending map[string]chan bridgeReply
}

type bridgeReply struct {
	payload json.RawMessage
	err     error
}

// NewBridge 创建前端桥。emit 通常是 wailsruntime.EventsEmit 的封装。
func NewBridge(emit func(event string, payload any)) *Bridge {
	return &Bridge{
		emit:    emit,
		timeout: 10 * time.Second,
		pending: make(map[string]chan bridgeReply),
	}
}

// SetTimeout 调整等待前端回包的超时时间。
func (b *Bridge) SetTimeout(d time.Duration) {
	if d > 0 {
		b.timeout = d
	}
}

// Call 请求前端执行 op，返回前端给的 JSON 结果。
func (b *Bridge) Call(ctx context.Context, op string, args any) (json.RawMessage, error) {
	if b.emit == nil {
		return nil, errors.New("前端桥未初始化")
	}
	id := fmt.Sprintf("dbg-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&b.seq, 1))
	ch := make(chan bridgeReply, 1)

	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	payload, err := json.Marshal(map[string]any{"id": id, "op": op, "args": args})
	if err != nil {
		return nil, fmt.Errorf("序列化前端请求失败: %w", err)
	}
	b.emit(BridgeRequestEvent, json.RawMessage(payload))

	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case rep := <-ch:
		return rep.payload, rep.err
	case <-timer.C:
		return nil, fmt.Errorf("前端响应超时（op=%s）: %w", op, context.DeadlineExceeded)
	case <-ctx.Done():
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
		return
	}
	var envelope struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		ch <- bridgeReply{err: fmt.Errorf("前端回包不是合法 JSON: %w", err)}
		return
	}
	if !envelope.OK {
		msg := envelope.Error
		if msg == "" {
			msg = "前端执行失败"
		}
		ch <- bridgeReply{err: errors.New(msg)}
		return
	}
	ch <- bridgeReply{payload: envelope.Data}
}
