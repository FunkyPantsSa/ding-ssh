package apphost

import (
	"context"
)

// SessionTargetLocal resolves the actual existing target without reconnecting
// or creating it. Closed local tabs still need exec.local on reconnect.
func (c *debugController) SessionTargetLocal(ctx context.Context, id string) (bool, bool, error) {
	if c.app.local != nil && c.app.local.Has(id) {
		return true, true, nil
	}
	if c.app.manager != nil {
		v := c.app.manager.SessionState(id)
		if v["exists"] == true {
			return false, true, nil
		}
	}
	if c.bridge == nil {
		return false, false, nil
	}
	v, err := c.callFrontend(ctx, "session.describe", map[string]any{"id": id})
	if err != nil {
		return false, false, err
	}
	m, ok := v.(map[string]any)
	if !ok || m["tabExists"] != true {
		return false, false, nil
	}
	return m["local"] == true, true, nil
}

func (c *debugController) sessionSnapshot(ctx context.Context, id string) (map[string]any, error) {
	state := c.sessionJobView(id)
	state["tabId"] = id
	state["backendConnected"] = state["status"] == "connected"
	if c.bridge == nil {
		state["tabExists"] = state["exists"]
		if state["exists"] != true {
			state["closeState"] = "closed"
		}
		return state, nil
	}
	v, err := c.callFrontend(ctx, "session.describe", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]any); ok {
		for key, value := range m {
			state[key] = value
		}
	}
	state["exists"] = state["tabExists"] == true || state["backendConnected"] == true
	return state, nil
}

var _ interface {
	SessionTargetLocal(context.Context, string) (bool, bool, error)
} = (*debugController)(nil)
