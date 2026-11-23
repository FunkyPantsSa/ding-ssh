package sshx

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"ding-ssh/internal/models"
)

// SessionState is a safe observation of the exact backend connection identity.
func (m *Manager) SessionState(id string) map[string]any {
	m.mu.RLock()
	s := m.sessions[id]
	_, pending := m.pending[id]
	m.mu.RUnlock()
	if s == nil {
		status := "closed"
		if pending {
			status = "connecting"
		}
		return map[string]any{"sessionId": id, "status": status, "exists": pending, "local": false}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "connected"
	if s.reconnecting {
		status = "connecting"
	} else if s.closed {
		status = "disconnected"
	} else if s.closing {
		status = "closed"
	}
	return map[string]any{"sessionId": id, "serverId": s.server.ID, "serverName": s.server.Name, "host": s.server.Host, "user": s.server.User, "status": status, "exists": true, "local": false, "createdAt": s.createdAt, "independentExecSupported": !s.server.JumpServerMode}
}

// CollectSnapshot reuses the fixed Linux collector script on a bounded exec
// channel; a first sample has counters but cannot establish network rates.
func (m *Manager) CollectSnapshot(ctx context.Context, id string) (models.SysInfoSnapshot, error) {
	p, err := m.PrepareExec(ExecRequest{SessionID: id, Command: sysInfoScript, Mode: "posix"})
	if err != nil {
		return models.SysInfoSnapshot{}, err
	}
	var mu sync.Mutex
	buf := make([]byte, 0, 64<<10)
	truncated := false
	result := p.Run(ctx, func(stream string, b []byte) {
		if stream != "stdout" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		left := (1 << 20) - len(buf)
		if len(b) > left {
			b = b[:left]
			truncated = true
		}
		buf = append(buf, b...)
	})
	mu.Lock()
	raw := string(buf)
	wasTruncated := truncated
	mu.Unlock()
	if raw == "" {
		if result.Error != "" {
			return models.SysInfoSnapshot{}, errors.New(result.Error)
		}
		return models.SysInfoSnapshot{}, errors.New("无系统指标输出；仅支持 Linux / procps top")
	}
	snap, _, counters, ips := parseSysInfoLoose(raw)
	snap.SessionID = id
	snap.CollectedAt = time.Now().UnixMilli()
	for _, c := range counters {
		snap.NetIfaces = append(snap.NetIfaces, models.NetIface{Name: c.name, IP: ips[c.name]})
	}
	if wasTruncated {
		snap.Error = "采集输出超过 1 MiB，部分字段不可用"
	} else if !snap.TopAvailable {
		snap.Error = "top 不可用或平台不支持；保留可解析的部分指标"
	}
	return snap, nil
}

// PendingHostTrustChallenges exposes fingerprints, never keys or approval power.
func PendingHostTrustChallenges() []HostTrustChallenge {
	challengeMu.Lock()
	defer challengeMu.Unlock()
	out := []HostTrustChallenge{}
	now := time.Now()
	for id, p := range challenges {
		if now.After(p.expires) {
			delete(challenges, id)
			continue
		}
		v := p.view
		v.OldFingerprints = append([]string{}, v.OldFingerprints...)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}
