package debugsrv

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBridgeTimeoutPreservesRecoveryMetadataAndHealthStats(t *testing.T) {
	bridge := NewBridge(func(string, any) {})
	bridge.SetTimeout(time.Millisecond)
	_, err := bridge.Call(context.Background(), OpUIAct, nil)
	var operation *OperationError
	if !errors.As(err, &operation) || operation.Code != "FRONTEND_TIMEOUT" || !operation.OutcomeUnknown || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout lost operation recovery details: %v", err)
	}
	if meta := toolErrorMeta(err); meta["outcomeKnown"] != false || len(operation.NextSteps) == 0 {
		t.Fatalf("timeout must warn against blindly repeating an action: %#v", meta)
	}
	stats := bridge.Stats()
	if stats["state"] != "degraded" || stats["pending"] != 0 || stats["failureCount"] != uint64(1) || stats["lastFailureAt"].(int64) <= 0 {
		t.Fatalf("timeout did not update bridge health: %#v", stats)
	}
}
