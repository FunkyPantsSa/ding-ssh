package debugsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"time"
)

// CaptureFrame describes the pixels actually captured. CSS coordinates are
// mapped using the measured bitmap/content ratio, never guessed from DPR.
type CaptureFrame struct {
	PNG               []byte  `json:"-"`
	Provider          string  `json:"provider"`
	Surface           string  `json:"surface"`
	WindowID          string  `json:"windowId"`
	PixelWidth        int     `json:"pixelWidth"`
	PixelHeight       int     `json:"pixelHeight"`
	ScaleX            float64 `json:"scaleX"`
	ScaleY            float64 `json:"scaleY"`
	OffsetX           float64 `json:"offsetX"`
	OffsetY           float64 `json:"offsetY"`
	ContentX          float64 `json:"contentX,omitempty"`
	ContentY          float64 `json:"contentY,omitempty"`
	ContentWidth      float64 `json:"contentWidth,omitempty"`
	ContentHeight     float64 `json:"contentHeight,omitempty"`
	OcclusionPossible bool    `json:"occlusionPossible"`
	CapturedAt        int64   `json:"capturedAt"`
	// Internal alignment evidence, never serialized as part of provider metadata.
	Observation map[string]any `json:"-"`
	After       map[string]any `json:"-"`
}

// CaptureProvider is injected by the desktop host. Tests can supply pixels
// without a GUI, and transport code never guesses an OS window by its title.
type CaptureProvider interface {
	Capture(context.Context, string) (CaptureFrame, error)
	CaptureCapabilities(context.Context) map[string]any
}

var safeCaptureSlot = make(chan struct{}, 1)

func (s *Server) captureFrame(ctx context.Context, windowID string) (CaptureFrame, error) {
	select {
	case safeCaptureSlot <- struct{}{}:
	case <-ctx.Done():
		return CaptureFrame{}, ctx.Err()
	}
	defer func() { <-safeCaptureSlot }()
	// Renderer masking protects moving/new form controls as well as the older
	// screenshot APIs. A missing bridge never falls back to unredacted pixels.
	prepared, err := uiFetch(ctx, s.uiFrontend(), "ui.capturePrepare", nil)
	if err != nil {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_REDACTION_UNAVAILABLE")
	}
	token := uiArgString(prepared, "captureToken")
	// Install release cleanup immediately after receiving a token. Every
	// validation below may fail (missing geometry, stale frames, malformed
	// lock), and leaving the renderer mask until the frontend watchdog creates
	// an avoidable capture lock and can hide controls longer than necessary.
	if token != "" {
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = uiFetch(releaseCtx, s.uiFrontend(), "ui.captureRelease", map[string]any{"captureToken": token})
		}()
	}
	if token == "" || prepared["accepted"] != true {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_REDACTION_UNAVAILABLE")
	}
	preparedRects, hasRects := prepared["sensitiveRects"].([]any)
	preparedLock := uiMap(prepared["captureLock"])
	if !hasRects || uiMap(prepared["observation"]) == nil || preparedLock["maskMode"] != "renderer-opacity" || uiNum(preparedLock["frames"]) < 2 {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_REDACTION_GEOMETRY_UNAVAILABLE")
	}
	frame, err := s.captureRawFrame(ctx, windowID)
	if err != nil {
		return CaptureFrame{}, err
	}
	after, err := uiFetch(ctx, s.uiFrontend(), "ui.diagnostics", nil)
	lock := uiMap(after["captureLock"])
	before := uiMap(prepared["observation"])
	if err != nil || before == nil || !uiBool(lock, "active") || uiArgString(lock, "token") != token || lock["maskMode"] != "renderer-opacity" || uiNum(lock["frames"]) < 2 || uiNum(lock["expiresAt"]) <= float64(time.Now().UnixMilli()) || before["pageEpoch"] != after["pageEpoch"] {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_REDACTION_EXPIRED")
	}
	if len(frame.PNG) > 32<<20 {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_TOO_LARGE")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(frame.PNG))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32<<20 {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_INVALID_OR_TOO_LARGE")
	}
	if frame.PixelWidth > 0 && frame.PixelWidth != cfg.Width || frame.PixelHeight > 0 && frame.PixelHeight != cfg.Height {
		return CaptureFrame{}, fmt.Errorf("CAPTURE_DIMENSION_MISMATCH")
	}
	// The host adapter's sensitiveRects are the canonical lock-time sample.
	// Require it even when empty, so older/partial bridges cannot bypass masks.
	before["sensitiveRects"] = preparedRects
	frame.Observation, frame.After = before, after
	return frame, nil
}

func (s *Server) captureRawFrame(ctx context.Context, windowID string) (CaptureFrame, error) {
	if windowID == "" {
		windowID = "main"
	}
	if provider, ok := s.opts.Handler.(CaptureProvider); ok {
		return provider.Capture(ctx, windowID)
	}
	// The legacy fallback is retained for embedding hosts without an adapter.
	if windowID != "main" {
		return CaptureFrame{}, fmt.Errorf("WINDOW_NOT_FOUND: %s", windowID)
	}
	png, err := captureWindowPNG()
	if err != nil {
		return CaptureFrame{}, err
	}
	return CaptureFrame{PNG: png, Provider: "legacy-window", Surface: "native-window", WindowID: windowID, OcclusionPossible: true, CapturedAt: time.Now().UnixMilli()}, nil
}

func captureMetadata(frame CaptureFrame) map[string]any {
	b, _ := json.Marshal(frame)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func uiCaptureScale(frame CaptureFrame, w, h int, geo map[string]any) uiScaleCalc {
	if frame.Surface == "webview" || frame.Surface == "client-area" {
		vp := uiMap(geo["viewport"])
		sx, sy := frame.ScaleX, frame.ScaleY
		if sx <= 0 && uiNum(vp["width"]) > 0 {
			sx = float64(w) / uiNum(vp["width"])
		}
		if sy <= 0 && uiNum(vp["height"]) > 0 {
			sy = float64(h) / uiNum(vp["height"])
		}
		if sx > 0 && sy > 0 {
			return uiScaleCalc{ImageW: w, ImageH: h, ScaleX: sx, ScaleY: sy, DPR: uiNum(vp["dpr"]), UIScale: uiNum(vp["uiScale"]), ChromeX: frame.OffsetX / sx, ChromeY: frame.OffsetY / sy}
		}
	}
	return uiComputeScale(w, h, geo)
}
