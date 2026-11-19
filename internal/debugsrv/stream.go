package debugsrv

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
)

// 事件总线是终端输出 / 传输进度的热路径（每个分块都可能 Publish 一次），
// 因此「广播事件」只做采样 DEBUG，「订阅者跟不上导致丢弃」用采样 WARN。
// SSE 逐事件推送的限流器在 server.go / mcp.go 共用同一个采样器。
var (
	publishLog     = logx.NewSampler(256, 2*time.Second)
	publishDropLog = logx.NewSampler(32, 5*time.Second)
	streamEventLog = logx.NewSampler(128, 2*time.Second)
)

// Event 一条推送给订阅者的事件。
type Event struct {
	Topic     string          `json:"topic"`               // 例如 ssh.output / ssh.status / tunnel.status
	SessionID string          `json:"sessionId,omitempty"` // 会话/标签 id（无则空）
	Name      string          `json:"name"`                // 原始事件名（如 ssh:output:tab-1）
	Data      json.RawMessage `json:"data,omitempty"`      // 原始 payload
	TS        int64           `json:"ts"`                  // 毫秒时间戳
}

// Hub 后端事件总线：把会话输出 / 状态 / 进度等广播给 SSE 订阅者。
// 用于 M2 的实时流（GET /v1/stream），并保留最近事件供 MCP 的 recent_events 工具查询。
type Hub struct {
	mu             sync.Mutex
	seq            int
	subs           map[int]*hubSub
	recent         []Event // 环形缓冲（最多 hubRecentLimit 条）
	transferRecent []Event // 传输事件单独保留，避免被终端输出挤掉
}

// hubRecentLimit 环形缓冲容量。
const hubRecentLimit = 300

type hubSub struct {
	topics map[string]bool // 空表示全部
	ch     chan Event
}

// NewHub 创建事件总线。
func NewHub() *Hub {
	return &Hub{subs: make(map[int]*hubSub)}
}

// Subscribe 订阅事件；topics 为空表示订阅全部。返回订阅 id 与接收通道。
func (h *Hub) Subscribe(topics ...string) (int, <-chan Event) {
	set := make(map[string]bool, len(topics))
	for _, t := range topics {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	h.mu.Lock()
	h.seq++
	id := h.seq
	ch := make(chan Event, 256)
	h.subs[id] = &hubSub{topics: set, ch: ch}
	total := len(h.subs)
	h.mu.Unlock()
	logx.Debugf("[debugsrv/stream] 新增订阅: sub=%d topics=%v total=%d", id, topics, total)
	return id, ch
}

// Unsubscribe 取消订阅并关闭通道。
func (h *Hub) Unsubscribe(id int) {
	h.mu.Lock()
	sub := h.subs[id]
	delete(h.subs, id)
	remaining := len(h.subs)
	h.mu.Unlock()
	logx.Debugf("[debugsrv/stream] 取消订阅: sub=%d exists=%v remaining=%d", id, sub != nil, remaining)
	if sub != nil {
		close(sub.ch)
	}
}

// Publish 广播事件；订阅者通道满时丢弃该事件（不阻塞会话读取）。
func (h *Hub) Publish(ev Event) {
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	h.mu.Lock()
	if ev.Topic == TransferTopic {
		h.transferRecent = append(h.transferRecent, ev)
		if len(h.transferRecent) > hubRecentLimit {
			h.transferRecent = h.transferRecent[len(h.transferRecent)-hubRecentLimit:]
		}
	}
	h.recent = append(h.recent, ev)
	if len(h.recent) > hubRecentLimit {
		h.recent = h.recent[len(h.recent)-hubRecentLimit:]
	}
	subs := len(h.subs)
	dropped := 0
	for _, sub := range h.subs {
		if len(sub.topics) > 0 && !sub.topics[ev.Topic] {
			continue
		}
		select {
		case sub.ch <- ev:
		default: // 订阅者跟不上：丢弃，保证读取协程不被阻塞
			dropped++
		}
	}
	h.mu.Unlock()
	// 日志在解锁之后写：日志写入可能阻塞（stderr / 磁盘），不能占着事件总线的锁。
	logx.DebugSampled(publishLog, "[debugsrv/stream] 广播事件: topic=%s session=%s subs=%d bytes=%d",
		ev.Topic, ev.SessionID, subs, len(ev.Data))
	if dropped > 0 {
		logx.WarnSampled(publishDropLog, "[debugsrv/stream] 订阅队列已满，丢弃事件: topic=%s dropped=%d subs=%d",
			ev.Topic, dropped, subs)
	}
}

// Recent 返回最近的 limit 条事件（可按 topic 过滤），最新的在最后。
func (h *Hub) Recent(limit int, topics ...string) []Event {
	if limit <= 0 || limit > hubRecentLimit {
		limit = 50
	}
	filter := make(map[string]bool, len(topics))
	for _, t := range topics {
		if t = strings.TrimSpace(t); t != "" {
			filter[t] = true
		}
	}
	h.mu.Lock()
	source := h.recent
	if len(filter) == 1 && filter[TransferTopic] {
		source = h.transferRecent
	}
	var out []Event
	for i := len(source) - 1; i >= 0 && len(out) < limit; i-- {
		ev := source[i]
		if len(filter) > 0 && !filter[ev.Topic] {
			continue
		}
		out = append(out, ev)
	}
	// 反转为时间正序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	h.mu.Unlock()
	logx.Debugf("[debugsrv/stream] 读取最近事件: limit=%d topics=%v count=%d", limit, topics, len(out))
	return out
}

// PublishRaw 把后端事件名（如 ssh:output:tab-1）拆成 topic/sessionId 后广播。
func (h *Hub) PublishRaw(name string, payload any) {
	topic, sessionID := SplitEventName(name)
	raw, err := json.Marshal(payload)
	if err != nil {
		logx.Warnf("[debugsrv/stream] 事件序列化失败，已丢弃: name=%s err=%v", name, err)
		return
	}
	h.Publish(Event{Topic: topic, SessionID: sessionID, Name: name, Data: raw})
}

// SplitEventName 解析后端事件名：ssh:output:<id> → ("ssh.output", "<id>")；
// tunnel:status → ("tunnel.status", "")。
func SplitEventName(name string) (topic, sessionID string) {
	parts := strings.Split(name, ":")
	if len(parts) < 2 {
		return name, ""
	}
	topic = parts[0] + "." + parts[1]
	if len(parts) > 2 {
		sessionID = strings.Join(parts[2:], ":")
	}
	return topic, sessionID
}
