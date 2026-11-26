package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
)

type verifyFixture struct {
	dir                                                                   string
	runtimeError, missingErrors, notReady, missingComponents, skipStorage bool
}

func (f *verifyFixture) JobStateDirectory() string { return f.dir }
func (f *verifyFixture) RuntimeObservation(context.Context) map[string]any {
	return map[string]any{"instanceId": "verify-instance", "build": map[string]any{"binarySHA256": "fixture"}}
}
func (f *verifyFixture) CaptureCapabilities(context.Context) map[string]any {
	return map[string]any{"available": true}
}
func (f *verifyFixture) Capture(context.Context, string) (CaptureFrame, error) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	png, err := uiEncodePNG(img)
	return CaptureFrame{PNG: png, Provider: "fixture", Surface: "webview", WindowID: "main", PixelWidth: 32, PixelHeight: 24, ScaleX: 1, ScaleY: 1}, err
}
func (f *verifyFixture) Handle(ctx context.Context, op string, args json.RawMessage) (any, error) {
	seq := map[string]any{"mutation": 1, "layout": 1}
	switch op {
	case OpUIObserve:
		d := map[string]any{"observationId": "verify-fixture-observation", "windowId": "main", "pageEpoch": "epoch", "ready": !f.notReady, "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}, "sensitiveRects": []any{}, "nodes": []any{}, "issues": []any{}, "errors": []any{}, "coverage": map[string]any{"components": "public-registry"}}
		if f.runtimeError {
			d["errors"] = []any{map[string]any{"code": "JS_TYPE", "source": "vue"}}
		}
		if f.missingErrors {
			delete(d, "errors")
		}
		if f.skipStorage {
			d["storageBudgetFixture"] = strings.Repeat("x", 600<<10)
		}
		return d, nil
	case "ui.diagnostics":
		return map[string]any{"pageEpoch": "epoch", "ready": true, "sequences": seq, "sensitiveRects": []any{}, "viewport": map[string]any{"width": 32, "height": 24}, "captureLock": map[string]any{"active": true, "token": "fixture-lock", "expiresAt": float64(9999999999999), "maskMode": "renderer-opacity", "frames": 2}}, nil
	case "ui.capturePrepare":
		return map[string]any{"accepted": true, "captureToken": "fixture-lock", "expiresAt": float64(9999999999999), "sensitiveRects": []any{}, "captureLock": map[string]any{"active": true, "token": "fixture-lock", "maskMode": "renderer-opacity", "frames": 2}, "observation": map[string]any{"pageEpoch": "epoch", "sequences": seq, "viewport": map[string]any{"width": 32, "height": 24}, "sensitiveRects": []any{}, "nodes": []any{}}}, nil
	case "ui.captureRelease":
		return map[string]any{"released": true}, nil
	case OpUITree:
		nodes := []any{map[string]any{"name": "App"}}
		if f.missingComponents {
			nodes = nil
		}
		return map[string]any{"source": "public-component-registry", "ready": true, "nodes": nodes}, nil
	}
	return nil, fmt.Errorf("unexpected fixture operation: %s", op)
}

func runVerificationReport(t *testing.T, f *verifyFixture, scenario string) map[string]any {
	t.Helper()
	f.dir = t.TempDir()
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	ctx := context.Background()
	content, err := s.handleVerify(ctx, OpVerifyRun, map[string]any{"scenarioId": scenario})
	if err != nil {
		t.Fatal(err)
	}
	var started map[string]any
	if err = json.Unmarshal([]byte(uiArgString(uiMap(content[0]), "text")), &started); err != nil {
		t.Fatal(err)
	}
	jobID := uiArgString(started, "jobId")
	jobsJSON(t, s, ctx, OpJobsWait, map[string]any{"jobId": jobID, "timeoutMs": 3000})
	out := jobsJSON(t, s, ctx, OpJobsOutput, map[string]any{"jobId": jobID, "maxBytes": 262144})
	var text strings.Builder
	for _, chunk := range uiMapSlice(out["chunks"]) {
		text.WriteString(uiArgString(chunk, "text"))
	}
	var report map[string]any
	if err = json.Unmarshal([]byte(text.String()), &report); err != nil {
		t.Fatalf("report decode: %v; %s", err, text.String())
	}
	return report
}

func TestVerifyRuntimeErrorsAndUnknownErrorCoverageCannotPass(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture verifyFixture
		status  string
	}{{"clear", verifyFixture{}, "passed"}, {"runtime-error", verifyFixture{runtimeError: true}, "failed"}, {"unknown-errors", verifyFixture{missingErrors: true}, "inconclusive"}, {"not-ready", verifyFixture{notReady: true}, "inconclusive"}} {
		t.Run(tc.name, func(t *testing.T) {
			report := runVerificationReport(t, &tc.fixture, "desktop.current")
			if report["status"] != tc.status {
				t.Fatalf("verification=%+v", report)
			}
		})
	}
}

func TestVerifyBootRequiresRegisteredComponents(t *testing.T) {
	if report := runVerificationReport(t, &verifyFixture{missingComponents: true}, "desktop.boot"); report["status"] != "inconclusive" {
		t.Fatalf("boot verification=%+v", report)
	}
	if report := runVerificationReport(t, &verifyFixture{}, "desktop.boot"); report["status"] != "passed" {
		t.Fatalf("boot verification=%+v", report)
	}
}

func TestVerifyArtifactFlagsReflectActualObservationStorage(t *testing.T) {
	for _, skip := range []bool{false, true} {
		report := runVerificationReport(t, &verifyFixture{skipStorage: skip}, "desktop.current")
		artifacts := uiMap(report["artifacts"])
		if artifacts["observationStored"] != !skip || artifacts["imageStored"] != !skip {
			t.Fatalf("skipStorage=%v artifacts=%+v", skip, artifacts)
		}
	}
}
