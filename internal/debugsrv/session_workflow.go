package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func isSessionWorkflowTool(name string) bool {
	switch name {
	case OpSessionsConnect, OpSessionsDisconnect, OpSessionsReconnect, OpSessionsClose, OpSessionsCloseStatus:
		return true
	}
	return false
}

func (s *Server) handleSessionWorkflow(ctx context.Context, name string, args map[string]any) ([]any, error) {
	def := 10000
	if name == OpSessionsCloseStatus {
		def = 0
	}
	waitMS, err := jobArgInt(args, "waitMs", def, 0, 30000)
	if err != nil {
		return nil, err
	}
	id := appctlString(args, "sessionId")
	if name != OpSessionsConnect && id == "" {
		return nil, fmt.Errorf("%w: 缺少 sessionId", ErrBadInput)
	}
	initial := map[string]any{"sessionId": id, "tabId": id}
	if name != OpSessionsCloseStatus {
		original := "open_tab"
		callArgs := map[string]any{"id": id}
		switch name {
		case OpSessionsConnect:
			server := appctlString(args, "serverId")
			local := appctlBool(args, "local")
			if server == "" && !local || server != "" && local {
				return nil, fmt.Errorf("%w: 必须指定 serverId 或 local=true，不能同时指定", ErrBadInput)
			}
			callArgs = map[string]any{"serverId": server, "local": local}
		case OpSessionsDisconnect:
			original = "disconnect_session"
		case OpSessionsReconnect:
			original = "reconnect_session"
		case OpSessionsClose:
			original = "close_tab"
		}
		content, _, callErr := s.mcpCallToolAudited(ctx, original, callArgs)
		if callErr != nil {
			return nil, callErr
		}
		if len(content) > 0 {
			if item, ok := content[0].(map[string]any); ok {
				if raw, ok := item["text"].(string); ok {
					_ = json.Unmarshal([]byte(raw), &initial)
				}
			}
		}
		if actual := appctlString(initial, "sessionId"); actual != "" {
			id = actual
		}
		if id == "" {
			return nil, fmt.Errorf("session operation did not return sessionId")
		}
	}
	initial["sessionId"] = id
	if appctlString(initial, "tabId") == "" {
		initial["tabId"] = id
	}
	return s.waitSessionWorkflow(ctx, name, id, initial, time.Duration(waitMS)*time.Millisecond)
}

func (s *Server) waitSessionWorkflow(ctx context.Context, name, id string, initial map[string]any, budget time.Duration) ([]any, error) {
	started := time.Now()
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	tick := time.NewTicker(75 * time.Millisecond)
	defer tick.Stop()
	state := initial
	finish := func(reason string, matched bool) ([]any, error) {
		state["matched"] = matched
		state["reason"] = reason
		state["elapsedMs"] = time.Since(started).Milliseconds()
		return jobJSON(state)
	}
	for {
		raw, _ := json.Marshal(map[string]any{"sessionId": id})
		v, err := s.opts.Handler.Handle(ctx, OpSessionSnapshot, raw)
		if err != nil {
			return nil, err
		}
		if snapshot, ok := v.(map[string]any); ok && snapshot != nil {
			for key, value := range snapshot {
				state[key] = value
			}
		}
		status := appctlString(state, "status")
		if name == OpSessionsClose || name == OpSessionsCloseStatus {
			closeState := appctlString(state, "closeState")
			if closeState == "confirmation_required" {
				state["nextStep"] = map[string]any{"action": "user_confirmation", "tool": OpSessionsCloseStatus, "args": map[string]any{"sessionId": id}, "message": "请用户在应用内确认关闭该标签，再调用 sessions.closeStatus 观察完成；MCP 不可代为批准。"}
				return finish(closeState, false)
			}
			if closeState == "cancelled" {
				return finish("cancelled", false)
			}
			if state["tabExists"] == false && state["backendConnected"] == false {
				state["closeState"] = "closed"
				return finish("closed", true)
			}
		} else if name == OpSessionsDisconnect {
			if state["backendConnected"] == false {
				return finish("disconnected", true)
			}
		} else {
			if status == "connected" && state["backendConnected"] != false {
				return finish("connected", true)
			}
			if status == "error" || status == "closed" || status == "disconnected" || state["tabExists"] == false {
				return finish("connection_failed", false)
			}
		}
		if budget == 0 {
			return finish("pending", false)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			want := "connected"
			if name == OpSessionsDisconnect {
				want = "closed"
			}
			state["nextStep"] = map[string]any{"tool": OpSessionsWait, "args": map[string]any{"sessionId": id, "status": want, "timeoutMs": 10000}}
			if name == OpSessionsClose || name == OpSessionsCloseStatus {
				state["nextStep"] = map[string]any{"tool": OpSessionsCloseStatus, "args": map[string]any{"sessionId": id, "waitMs": 10000}}
			}
			return finish("timeout", false)
		case <-tick.C:
		}
	}
}
