package debugsrv

import (
	"context"
	"image"
	"image/color"
	"testing"
)

func TestObservationCoordinatesAlignedRequiresMatchingPageViewportAndLayout(t *testing.T) {
	base := map[string]any{
		"pageEpoch": "page-1",
		"viewport":  map[string]any{"width": 1280, "height": 800, "dpr": 2, "uiScale": 1},
		"sequences": map[string]any{"mutation": 4, "layout": 9, "frame": 12},
	}
	if !observationCoordinatesAligned(base, base) {
		t.Fatal("identical semantic and capture observations should align")
	}
	for name, mutate := range map[string]func(map[string]any){
		"page":             func(v map[string]any) { v["pageEpoch"] = "page-2" },
		"viewport":         func(v map[string]any) { uiMap(v["viewport"])["dpr"] = 1 },
		"layout":           func(v map[string]any) { uiMap(v["sequences"])["layout"] = 10 },
		"mutation":         func(v map[string]any) { uiMap(v["sequences"])["mutation"] = 5 },
		"missing-viewport": func(v map[string]any) { delete(v, "viewport") },
	} {
		t.Run(name, func(t *testing.T) {
			changed := map[string]any{
				"pageEpoch": base["pageEpoch"],
				"viewport":  map[string]any{"width": 1280, "height": 800, "dpr": 2, "uiScale": 1},
				"sequences": map[string]any{"mutation": 4, "layout": 9, "frame": 12},
			}
			mutate(changed)
			if observationCoordinatesAligned(base, changed) {
				t.Fatalf("semantic and capture observations incorrectly aligned: %+v", changed)
			}
		})
	}
}

func TestObservationBudgetIncludesActionWaitTimeout(t *testing.T) {
	got, err := observationBudget(OpUIAct, map[string]any{
		"timeoutMs": 1000,
		"wait":      map[string]any{"timeoutMs": 15000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 20_000_000_000 {
		t.Fatalf("budget=%s, want 20s", got)
	}
	got, err = observationBudget(OpUIObserve, map[string]any{"timeoutMs": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if got != 6_000_000_000 {
		t.Fatalf("observe budget=%s, want 6s", got)
	}
	if _, err = observationBudget(OpUIAct, map[string]any{
		"wait": map[string]any{"timeoutMs": 15001},
	}); err == nil {
		t.Fatal("out-of-range wait timeout should fail validation")
	}
}

func compareFixtureData(ref, name string, x float64) map[string]any {
	return map[string]any{"observationId": "fixture", "windowId": "main", "pageEpoch": "epoch",
		"build": map[string]any{"commit": "fixture"}, "appearance": map[string]any{"colorScheme": "dark"},
		"viewport": map[string]any{"width": 32, "height": 24, "dpr": 1}, "sensitiveRects": []any{},
		"runtime":  map[string]any{"fonts": map[string]any{"supported": true, "status": "loaded", "faces": []any{}}},
		"capture":  map[string]any{"provider": "fixture", "surface": "webview", "outputPixelSize": map[string]any{"width": 32, "height": 24}, "cssToOutput": map[string]any{"scaleX": 1, "scaleY": 1, "offsetX": 0, "offsetY": 0}, "redactions": 0, "redactionMode": "renderer-opacity-and-pixel-mask", "consistency": "stable-window"},
		"nodes":    []any{map[string]any{"ref": ref, "role": "button", "name": name, "automationId": "", "rect": map[string]any{"x": x, "y": 1, "w": 10, "h": 10}, "visible": true, "actionable": true, "disabled": false, "inert": false}},
		"coverage": map[string]any{"truncated": false}}
}

func TestCompareRejectsUnstableOrDifferentCaptureEnvironment(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(d map[string]any) { uiMap(d["capture"])["consistency"] = "changed-during-capture" },
		func(d map[string]any) { uiMap(d["appearance"])["colorScheme"] = "light" },
		func(d map[string]any) { uiMap(d["runtime"])["fonts"] = map[string]any{"supported": false} },
	} {
		s := New(Options{})
		defer s.CloseJobs()
		a, b := compareFixtureData("n1", "Home", 1), compareFixtureData("n1", "Home", 1)
		b["observationId"] = "fixture-current"
		mutate(b)
		s.saveObservation(context.Background(), a, []byte("png"))
		s.saveObservation(context.Background(), b, []byte("png"))
		result, err := s.compareObservations(context.Background(), map[string]any{"baselineId": a["observationId"], "currentId": b["observationId"]})
		if err != nil {
			t.Fatal(err)
		}
		if uiBool(result.Data, "comparable") {
			t.Fatalf("incompatible observations were comparable: %+v", result.Data)
		}
	}
}

func TestCompareReportsStablePixelAndSemanticChanges(t *testing.T) {
	s := New(Options{})
	defer s.CloseJobs()
	imagePNG := func(c color.RGBA) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 32, 24))
		img.Set(1, 1, c)
		out, err := uiEncodePNG(img)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a, b := compareFixtureData("n1", "Home", 1), compareFixtureData("n1", "Home", 8)
	b["observationId"] = "fixture-current"
	uiMapSlice(b["nodes"])[0]["name"] = "Servers"
	s.saveObservation(context.Background(), a, imagePNG(color.RGBA{0, 0, 0, 255}))
	s.saveObservation(context.Background(), b, imagePNG(color.RGBA{255, 0, 0, 255}))
	result, err := s.compareObservations(context.Background(), map[string]any{"baselineId": a["observationId"], "currentId": b["observationId"]})
	if err != nil {
		t.Fatal(err)
	}
	if !uiBool(result.Data, "comparable") || len(uiMapSlice(uiMap(result.Data["semantics"])["changed"])) != 1 {
		t.Fatalf("comparison=%+v", result.Data)
	}
}
