package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"testing"
)

func TestObservationCaptureFailsClosedWithoutPaintedRendererLock(t *testing.T) {
	for _, lock := range []map[string]any{nil, {"active": true, "token": "lock", "maskMode": "renderer-opacity", "frames": 1}} {
		h := &captureFixture{lock: lock}
		s := New(Options{Handler: h})
		_, err := s.captureFrame(context.Background(), "main")
		if err == nil {
			t.Fatalf("capture accepted without a painted lock: %+v", lock)
		}
		if h.captured {
			t.Fatal("native capture ran without a painted redaction lock")
		}
		if !h.released {
			t.Fatal("capture token was not released after validation failure")
		}
	}
}

type captureFixture struct {
	lock     map[string]any
	captured bool
	released bool
	semantic map[string]any
}

func (f *captureFixture) Handle(_ context.Context, op string, _ json.RawMessage) (any, error) {
	seq := map[string]any{"mutation": 1, "layout": 1}
	switch op {
	case OpUIObserve:
		if f.semantic != nil {
			return f.semantic, nil
		}
		return map[string]any{"observationId": "semantic", "pageEpoch": "e", "ready": true, "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}, "nodes": []any{}, "issues": []any{}, "errors": []any{}, "coverage": map[string]any{}}, nil
	case "ui.capturePrepare":
		if f.lock == nil {
			return map[string]any{"accepted": true, "captureToken": "lock", "sensitiveRects": []any{}, "observation": map[string]any{"pageEpoch": "e", "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}}}, nil
		}
		return map[string]any{"accepted": true, "captureToken": "lock", "captureLock": f.lock, "sensitiveRects": []any{}, "observation": map[string]any{"pageEpoch": "e", "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}}}, nil
	case "ui.captureRelease":
		f.released = true
		return map[string]any{"released": true}, nil
	case "ui.diagnostics":
		return map[string]any{"pageEpoch": "e", "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}, "captureLock": map[string]any{"active": true, "token": "lock", "maskMode": "renderer-opacity", "frames": 2, "expiresAt": float64(9999999999999)}}, nil
	}
	return nil, fmt.Errorf("unexpected operation %s", op)
}
func (f *captureFixture) Capture(context.Context, string) (CaptureFrame, error) {
	f.captured = true
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	png, err := uiEncodePNG(img)
	return CaptureFrame{PNG: png, PixelWidth: 32, PixelHeight: 24, ScaleX: 1, ScaleY: 1}, err
}
func (*captureFixture) CaptureCapabilities(context.Context) map[string]any {
	return map[string]any{"available": true}
}

func TestObservationDowngradesWhenSemanticObservationDoesNotAlignWithCapture(t *testing.T) {
	h := &captureFixture{
		lock: map[string]any{"active": true, "token": "lock", "maskMode": "renderer-opacity", "frames": 2},
		semantic: map[string]any{
			"observationId": "semantic",
			"pageEpoch":     "e",
			"ready":         true,
			"sequences":     map[string]any{"mutation": 2, "layout": 1},
			"viewport":      map[string]any{"width": 32, "height": 24},
			"nodes":         []any{}, "issues": []any{}, "errors": []any{}, "coverage": map[string]any{},
		},
	}
	s := New(Options{Handler: h})
	defer s.CloseJobs()
	result, err := s.handleObservation(context.Background(), OpUIObserve, map[string]any{"includeImage": true})
	if err != nil {
		t.Fatal(err)
	}
	capture := uiMap(result.Data["capture"])
	if capture["coordinatesAligned"] != false || capture["consistency"] != "semantic-observation-misaligned" {
		t.Fatalf("misaligned capture was not downgraded: %+v", capture)
	}
	coverage := uiMap(result.Data["coverage"])
	if coverage["coordinatesAligned"] != false {
		t.Fatalf("coverage did not expose coordinate misalignment: %+v", coverage)
	}
}
