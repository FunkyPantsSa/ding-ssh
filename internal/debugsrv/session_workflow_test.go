package debugsrv

import (
	"context"
	"fmt"
	"testing"
)

func TestSessionConnectReturnsExactIDsAndWaitsForReady(t *testing.T) {
	ensureAllRegistrations(t)
	polls := 0
	h := &appctlStubHandler{replyFunc: func(op string, args map[string]any) (any, error) {
		switch op {
		case OpTabOpen:
			return map[string]any{"sessionId": "new-id", "tabId": "new-id", "local": true, "status": "connecting"}, nil
		case OpSessionSnapshot:
			if args["sessionId"] != "new-id" {
				t.Fatalf("wrong new target: %v", args)
			}
			polls++
			status := "connecting"
			if polls >= 2 {
				status = "connected"
			}
			return map[string]any{"status": status, "tabExists": true, "backendConnected": polls >= 2}, nil
		}
		return nil, fmt.Errorf("unexpected op %s", op)
	}}
	s := New(Options{Handler: h, Gate: &fakeGate{}})
	v := jobsJSON(t, s, context.Background(), OpSessionsConnect, map[string]any{"local": true, "waitMs": 1000})
	if v["sessionId"] != "new-id" || v["tabId"] != "new-id" || v["reason"] != "connected" || v["matched"] != true || polls != 2 {
		t.Fatalf("bad connect result: %v polls=%d", v, polls)
	}
}

func TestSessionCloseReportsApprovalAndObservesCleanup(t *testing.T) {
	ensureAllRegistrations(t)
	state := "confirmation_required"
	closed := false
	h := &appctlStubHandler{replyFunc: func(op string, args map[string]any) (any, error) {
		if op == OpTabClose {
			return map[string]any{"sessionId": "one", "tabId": "one"}, nil
		}
		if op == OpSessionSnapshot {
			return map[string]any{"closeState": state, "tabExists": !closed, "backendConnected": !closed, "status": "connected"}, nil
		}
		return nil, fmt.Errorf("unexpected op %s", op)
	}}
	s := New(Options{Handler: h, Gate: &fakeGate{}})
	v := jobsJSON(t, s, context.Background(), OpSessionsClose, map[string]any{"sessionId": "one"})
	if v["reason"] != "confirmation_required" || v["matched"] != false || v["nextStep"] == nil {
		t.Fatalf("lost user confirmation: %v", v)
	}
	state = "cancelled"
	v = jobsJSON(t, s, context.Background(), OpSessionsCloseStatus, map[string]any{"sessionId": "one"})
	if v["reason"] != "cancelled" || v["matched"] != false {
		t.Fatalf("lost cancellation: %v", v)
	}
	state, closed = "closed", true
	v = jobsJSON(t, s, context.Background(), OpSessionsCloseStatus, map[string]any{"sessionId": "one"})
	if v["reason"] != "closed" || v["matched"] != true {
		t.Fatalf("did not observe cleanup: %v", v)
	}
	count := 0
	for _, op := range h.calls {
		if op == OpTabClose {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("closeStatus mutated the tab: calls=%v", h.calls)
	}
}

func TestSessionWaitBudgetValidatesBeforeMutation(t *testing.T) {
	ensureAllRegistrations(t)
	for _, wait := range []any{-1, 0.5, 30001, "100"} {
		h := &appctlStubHandler{}
		s := New(Options{Handler: h, Gate: &fakeGate{}})
		if _, err := s.handleSessionWorkflow(context.Background(), OpSessionsConnect, map[string]any{"local": true, "waitMs": wait}); err == nil {
			t.Fatalf("accepted wait=%v", wait)
		}
		if len(h.calls) != 0 {
			t.Fatal("invalid budget mutated host")
		}
	}
}

func TestSessionCloseWaitsForBothTabAndBackendCleanup(t *testing.T) {
	ensureAllRegistrations(t)
	polls := 0
	h := &appctlStubHandler{replyFunc: func(op string, args map[string]any) (any, error) {
		if op == OpTabClose {
			return map[string]any{"sessionId": "one"}, nil
		}
		polls++
		return map[string]any{"tabExists": false, "backendConnected": polls < 2, "closeState": "closed"}, nil
	}}
	s := New(Options{Handler: h, Gate: &fakeGate{}})
	v := jobsJSON(t, s, context.Background(), OpSessionsClose, map[string]any{"sessionId": "one", "waitMs": 1000})
	if v["matched"] != true || polls != 2 {
		t.Fatalf("reported close before cleanup: %v polls=%d", v, polls)
	}
}
