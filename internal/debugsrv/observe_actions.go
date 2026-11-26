package debugsrv

import (
	"context"
	"ding-ssh/internal/desktopcapture"
	"fmt"
)

type nativeInputProvider interface {
	NativeInput(context.Context, string, desktopcapture.Input) (*desktopcapture.InputResult, error)
}

// Actions on the instrumented window are serialized across all MCP clients.
// Rejection is immediate; queued commands must not act on stale UI later.
var observationActionSlot = make(chan struct{}, 1)

func actionUnknown(operationID, mode string) map[string]any {
	return map[string]any{"operationId": operationID, "accepted": true, "dispatched": "unknown", "settled": false, "effectConfirmed": false, "outcome": "outcome-unknown", "retrySafe": false, "inputMode": mode, "error": map[string]any{"code": "ACTION_OUTCOME_UNKNOWN"}, "nextAction": "observe current state before deciding whether to retry"}
}

func (s *Server) dispatchObservedAction(ctx context.Context, args map[string]any, operationID string) (map[string]any, error) {
	mode := uiArgString(args, "inputMode")
	if mode == "" {
		mode = "dom-synthetic"
	}
	if mode != "dom-synthetic" && mode != "native-window-events" {
		return nil, fmt.Errorf("%w: unsupported inputMode", ErrBadInput)
	}
	select {
	case observationActionSlot <- struct{}{}:
	default:
		return map[string]any{"operationId": operationID, "accepted": false, "dispatched": false, "retrySafe": true, "error": map[string]any{"code": "WINDOW_ACTION_BUSY"}}, nil
	}
	defer func() { <-observationActionSlot }()
	if mode == "dom-synthetic" {
		r, e := uiFetch(ctx, s.uiFrontend(), OpUIAct, args)
		if e != nil {
			return actionUnknown(operationID, mode), nil
		}
		r["operationId"] = operationID
		r["retrySafe"] = r["accepted"] == false && r["dispatched"] == false
		return r, nil
	}
	provider, ok := s.opts.Handler.(nativeInputProvider)
	if !ok {
		return map[string]any{"operationId": operationID, "accepted": false, "dispatched": false, "error": map[string]any{"code": "NATIVE_INPUT_UNSUPPORTED"}}, nil
	}
	prepared, e := uiFetch(ctx, s.uiFrontend(), "ui.prepareAct", args)
	if e != nil {
		return map[string]any{"operationId": operationID, "accepted": false, "dispatched": false, "error": map[string]any{"code": "NATIVE_PREFLIGHT_FAILED"}}, nil
	}
	if prepared["accepted"] != true {
		prepared["operationId"] = operationID
		return prepared, nil
	}
	point, params := uiMap(prepared["point"]), uiMap(args["params"])
	input := desktopcapture.Input{Action: uiArgString(args, "action"), X: uiNum(point["x"]), Y: uiNum(point["y"]), Text: uiArgString(params, "text"), Key: uiArgString(params, "key"), Button: uiArgString(params, "button")}
	if values, ok := params["modifiers"].([]any); ok {
		for _, v := range values {
			if str, ok := v.(string); ok {
				input.Modifiers = append(input.Modifiers, str)
			}
		}
	}
	native, e := provider.NativeInput(ctx, "main", input)
	if e != nil {
		// Platform errors can arrive after a main-thread event was dispatched.
		// Never turn transport/callback timeout into a safe-to-retry rejection.
		r := actionUnknown(operationID, mode)
		if typed, ok := e.(*desktopcapture.Error); ok && (typed.Code == "WINDOW_NOT_FOCUSED" || typed.Code == "NATIVE_INPUT_UNSUPPORTED" || typed.Code == "NATIVE_ACTION_UNSUPPORTED" || typed.Code == "NATIVE_TEXT_UNSUPPORTED" || typed.Code == "NATIVE_KEY_UNSUPPORTED" || typed.Code == "NATIVE_BUTTON_UNSUPPORTED" || typed.Code == "INPUT_SIZE_LIMIT" || typed.Code == "NATIVE_MODIFIER_UNSUPPORTED") {
			r["accepted"], r["dispatched"], r["retrySafe"], r["outcome"] = false, false, true, "rejected"
			r["error"] = map[string]any{"code": typed.Code}
		}
		return r, nil
	}
	waitArgs := uiMap(args["wait"])
	if waitArgs == nil {
		waitArgs = map[string]any{"condition": map[string]any{"kind": "stable"}, "timeoutMs": 1000}
	}
	waited, e := uiFetch(ctx, s.uiFrontend(), OpUIWait, waitArgs)
	r := map[string]any{"operationId": operationID, "accepted": true, "dispatched": native.Delivered, "inputMode": native.InputMode, "provider": native.Provider, "trustedEvent": native.TrustedEvent, "limitations": native.Limitations, "retrySafe": false, "settled": false, "effectConfirmed": false}
	if e != nil {
		r["outcome"] = "outcome-unknown"
		r["error"] = map[string]any{"code": "POSTCONDITION_UNAVAILABLE"}
		return r, nil
	}
	r["observation"] = waited["observation"]
	r["settled"] = uiBool(uiMap(waited["stability"]), "settled")
	r["effectConfirmed"] = uiMap(args["wait"])["condition"] != nil && waited["matched"] == true
	r["effectBasis"] = "explicit-condition-required"
	r["condition"] = waited["condition"]
	return r, nil
}
