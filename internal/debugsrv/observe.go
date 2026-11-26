package debugsrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"sort"
	"time"
)

const (
	OpAppObserve          = "app.observe"
	OpRuntimeCapabilities = "app.runtimeCapabilities"
	OpDiagnostics         = "app.diagnostics"
	OpUIObserve           = "ui.observe"
	OpUIInspect           = "ui.inspect"
	OpUIDiagnose          = "ui.diagnose"
	OpUIWait              = "ui.wait"
	OpUIAct               = "ui.act"
	OpUICompare           = "ui.compare"
)

var pixelProcessingSlot = make(chan struct{}, 1)

type storedObservation struct {
	Owner string
	At    int64
	Data  map[string]any
	PNG   []byte
}

func registerObservationCaps() {
	for _, n := range []string{OpAppObserve, OpRuntimeCapabilities, OpDiagnostics, OpUIObserve, OpUIInspect, OpUIDiagnose, OpUIWait, OpUICompare} {
		RegisterToolCap(n, CapUIInspect)
	}
	RegisterToolCap(OpUIAct, CapUIWrite, CapTrustedAutomation)
	registerVerifyCaps()
	registerProbeCaps()
}
func init() { registerObservationCaps() }
func isObservationTool(n string) bool {
	switch n {
	case OpAppObserve, OpRuntimeCapabilities, OpDiagnostics, OpUIObserve, OpUIInspect, OpUIDiagnose, OpUIWait, OpUIAct, OpUICompare:
		return true
	}
	return false
}
func observationToolEntries() []mcpTool {
	target := map[string]any{"windowId": strProp("Wails窗口名，默认main"), "observationId": strProp("ui.observe返回的观测id"), "targetRef": strProp("观测中的稳定节点ref"), "selector": strProp("唯一CSS选择器，兼容高级调试")}
	props := func(extra map[string]any) map[string]any {
		p := map[string]any{}
		for k, v := range target {
			p[k] = v
		}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	flexible := map[string]any{"type": "object", "additionalProperties": true}
	observe := props(map[string]any{"includeImage": boolProp("默认true，包含真实像素图"), "limit": intProp("语义节点上限，默认100，最多500"), "timeoutMs": intProp("前端等待预算0–15000，默认1000"), "stableForMs": intProp("布局安静窗口0–1000，默认100"), "ignoreTerminalOutput": boolProp("持续输出时只等待布局"), "maxImageDimension": intProp("图像长边上限320–4096，默认1440")})
	list := []mcpTool{
		{Name: OpRuntimeCapabilities, Description: "查询当前实例的真实截图provider/平台/限制、语义定位和输入能力；available不是实际截图成功证明。", InputSchema: obj(props(nil))},
		{Name: OpAppObserve, Description: "AI默认入口：汇总宿主、桥接、前端、当前真实截图、语义节点、诊断候选和覆盖范围。前端或截图失败仍返回部分结果，不将未知当健康。", InputSchema: obj(observe)},
		{Name: OpUIObserve, Description: "采集当前真实WebView图、页面状态、可操作节点ref、字体/布局/终端诊断。校验采集前后序号；图与DOM不一致时明确标记。", InputSchema: obj(observe)},
		{Name: OpUIInspect, Description: "深入观测节点的盒模型、样式、裁切链、命中与归属组件。使用observationId/targetRef或唯一selector。", InputSchema: obj(props(nil))},
		{Name: OpUIDiagnose, Description: "有界扫描布局、遮挡、字体、图片、焦点和终端几何问题；区分事实与视觉候选。", InputSchema: obj(props(map[string]any{"limit": intProp("扫描节点上限1–2000")}))},
		{Name: OpUIWait, Description: "等待明确UI条件（kind:ready/fonts/view/element/terminal-output/stable），超时返回未满足条件和最后观测。", InputSchema: obj(props(map[string]any{"condition": flexible, "timeoutMs": intProp("最多15000ms"), "stableForMs": intProp("稳定窗口0–1000ms")}))},
		{Name: OpUIAct, Description: "可靠UI动作：唯一定位、过期校验、可见/遮挡检查、执行、等待条件并返回操作后画面。inputMode默认dom-synthetic；native-window-events按平台能力支持click/type/key，要求窗口已聚焦。超时可能已执行，返回结果未知，不自动重试。", InputSchema: obj(props(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"click", "type", "key", "hover", "scroll", "focus"}}, "inputMode": map[string]any{"type": "string", "enum": []string{"dom-synthetic", "native-window-events"}}, "params": flexible, "wait": flexible, "includeImage": boolProp("默认true，返回操作后截图")}), "action")},
		{Name: OpUICompare, Description: "对比两份已保存观测的像素与语义/几何变化；不同viewport/DPR/主题等环境明确不可比。仅当前身份的观测可访问。", InputSchema: obj(map[string]any{"baselineId": strProp("基线observationId"), "currentId": strProp("当前observationId"), "threshold": intProp("像素通道差阈值0–255，默认12")}, "baselineId", "currentId")},
		{Name: OpDiagnostics, Description: "按observationId读取当前身份的历史观测及过期标记，或读取当前错误/组件/性能安全诊断。不会返回任意日志对象或凭据。", InputSchema: obj(props(map[string]any{"includeImage": boolProp("历史观测是否返回图，默认false")}))},
	}
	for i := range list {
		list[i].OutputSchema = outputObj(nil)
		list[i].structured = structuredIdentity
	}
	return list
}

func (s *Server) runtimeObservation(ctx context.Context) map[string]any {
	if host, ok := s.opts.Handler.(interface {
		RuntimeObservation(context.Context) map[string]any
	}); ok {
		return host.RuntimeObservation(ctx)
	}
	return map[string]any{"backend": map[string]any{"state": "unknown", "reasonCode": "HOST_METRICS_UNAVAILABLE"}}
}
func (s *Server) runtimeCapabilities(ctx context.Context) map[string]any {
	pixels := map[string]any{"available": false, "reasonCode": "CAPTURE_PROVIDER_UNAVAILABLE"}
	if p, ok := s.opts.Handler.(CaptureProvider); ok {
		pixels = p.CaptureCapabilities(ctx)
	}
	native := map[string]any{"available": false, "reasonCode": "NATIVE_INPUT_UNSUPPORTED"}
	if p, ok := s.opts.Handler.(interface {
		NativeInputCapabilities(context.Context) map[string]any
	}); ok {
		native = p.NativeInputCapabilities(ctx)
	}
	return map[string]any{"schemaVersion": "1", "capture": pixels, "input": map[string]any{"semantic": true, "domSynthetic": true, "native": native, "trusted": "unverified"}, "observation": map[string]any{"partialResults": true, "maxStored": 64, "retentionSec": 3600, "maxBytes": 64 << 20}, "windows": []string{"main"}, "frontend": map[string]any{"componentSource": "public-component-registry", "actualGlyphFont": "unsupported", "network": "not-instrumented"}}
}
func (s *Server) handleObservation(ctx context.Context, name string, args map[string]any) (*UIResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	// The current event bridge is bound to the main surface. Reject other windows
	// rather than combining a detached window image with the main window's DOM.
	if id := uiArgString(args, "windowId"); id != "" && id != "main" {
		return nil, fmt.Errorf("%w: WINDOW_BRIDGE_UNAVAILABLE: only main is currently instrumented", ErrBadInput)
	}
	switch name {
	case OpRuntimeCapabilities:
		return uiResult(s.runtimeCapabilities(ctx)), nil
	case OpUICompare:
		return s.compareObservations(ctx, args)
	case OpDiagnostics:
		if id := uiArgString(args, "observationId"); id != "" {
			r, e := s.loadObservation(ctx, id)
			if e != nil {
				return nil, e
			}
			result := uiResult(map[string]any{"observation": r.Data, "ageMs": time.Now().UnixMilli() - r.At, "stale": true})
			if args["includeImage"] == true && len(r.PNG) > 0 {
				result.addImage("历史观测（非当前画面）", r.PNG)
			}
			return result, nil
		}
		return uiPassthroughTool(ctx, s.uiFrontend(), "ui.diagnostics", args, nil)
	case OpUIInspect, OpUIDiagnose:
		return uiPassthroughTool(ctx, s.uiFrontend(), name, args, nil)
	}
	budget, err := observationBudget(name, args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	host := map[string]any{}
	if name == OpAppObserve {
		host = s.runtimeObservation(ctx)
	}
	frontendArgs := map[string]any{}
	for _, k := range []string{"observationId", "targetRef", "selector", "limit", "timeoutMs", "stableForMs", "ignoreTerminalOutput", "action", "params", "wait", "condition"} {
		if v, ok := args[k]; ok {
			frontendArgs[k] = v
		}
	}
	op := name
	if op == OpAppObserve {
		op = OpUIObserve
	}
	started := time.Now()
	var fe map[string]any
	var feErr error
	if name == OpUIAct {
		frontendArgs["inputMode"] = args["inputMode"]
		fe, feErr = s.dispatchObservedAction(ctx, frontendArgs, "op_"+newSessionID())
	} else {
		fe, feErr = uiFetch(ctx, s.uiFrontend(), op, frontendArgs)
	}
	if feErr != nil {
		data := map[string]any{"schemaVersion": "1", "instanceId": host["instanceId"], "observationId": "obs_" + newSessionID(), "capturedAt": time.Now().UnixMilli(), "health": map[string]any{"state": "unknown"}, "coverage": map[string]any{"dom": "unavailable", "pixels": "notCaptured", "reasonCode": "FRONTEND_UNAVAILABLE"}, "frontend": map[string]any{"ready": false, "state": "unhealthy", "errorCode": "FRONTEND_UNAVAILABLE"}, "host": host, "elapsedMs": time.Since(started).Milliseconds()}
		return uiResult(data), nil
	}
	data := fe
	observation := fe
	if name == OpUIAct || name == OpUIWait {
		if o := uiMap(fe["observation"]); o != nil {
			observation = o
		}
	}
	if name == OpAppObserve {
		data = map[string]any{}
		for k, v := range fe {
			data[k] = v
		}
		data["host"] = host
		data["instanceId"] = host["instanceId"]
		data["frontend"] = map[string]any{"ready": uiBool(fe, "ready"), "state": "responsive"}
	}
	data["schemaVersion"] = "1"
	result := uiResult(data)
	includeImage := args["includeImage"] != false
	if name == OpUIAct && fe["accepted"] == false {
		includeImage = false
	}
	capture := map[string]any{"status": "notRequested"}
	if includeImage {
		frame, e := s.captureFrame(ctx, "main")
		if e != nil {
			capture = map[string]any{"status": "unavailable", "reason": uiClipString(e.Error(), 300)}
		} else {
			capture = captureMetadata(frame)
			capture["status"] = "captured"
			// A renderer lock hides sensitive controls throughout native capture.
			// Alignment is a separate claim, checked using the locked geometry.
			after := frame.After
			before := frame.Observation
			coordinatesAligned := observationCoordinatesAligned(observation, before)
			capture["coordinatesAligned"] = coordinatesAligned
			capture["consistency"] = "unknown"
			if after != nil && before != nil {
				beforeSeq := uiMap(before["sequences"])
				afterSeq := uiMap(after["sequences"])
				if beforeSeq != nil && afterSeq != nil && before["pageEpoch"] == after["pageEpoch"] && sameJSON(before["viewport"], after["viewport"]) && fmt.Sprint(beforeSeq["mutation"]) == fmt.Sprint(afterSeq["mutation"]) && fmt.Sprint(beforeSeq["layout"]) == fmt.Sprint(afterSeq["layout"]) {
					capture["consistency"] = "stable-window"
				} else {
					capture["consistency"] = "changed-during-capture"
				}
			}
			if !coordinatesAligned {
				// The lock-time observation is the coordinate basis for the
				// pixels. Never label the outer semantic nodes as a stable image
				// association when they came from a different page/viewport/seq.
				capture["consistency"] = "semantic-observation-misaligned"
			}
			png, redactions, e := prepareObservationPixels(frame, before, args)
			if e != nil {
				capture["status"] = "unavailable"
				capture["reason"] = e.Error()
			} else {
				capture["redactions"] = redactions
				capture["redactionMode"] = "renderer-opacity-and-pixel-mask"
				cfg, _ := pngConfig(png)
				inputCfg, _ := pngConfig(frame.PNG)
				if inputCfg.Width <= 0 || inputCfg.Height <= 0 {
					inputCfg = cfg
				}
				factorX, factorY := float64(cfg.Width)/float64(inputCfg.Width), float64(cfg.Height)/float64(inputCfg.Height)
				scaleX, scaleY, offsetX, offsetY := frame.ScaleX, frame.ScaleY, frame.OffsetX, frame.OffsetY
				vp := uiMap(before["viewport"])
				if scaleX <= 0 && uiNum(vp["width"]) > 0 {
					scaleX = float64(inputCfg.Width) / uiNum(vp["width"])
				}
				if scaleY <= 0 && uiNum(vp["height"]) > 0 {
					scaleY = float64(inputCfg.Height) / uiNum(vp["height"])
				}
				capture["outputPixelSize"] = map[string]int{"width": cfg.Width, "height": cfg.Height}
				if scaleX > 0 && scaleY > 0 {
					capture["cssToOutput"] = map[string]float64{"scaleX": scaleX * factorX, "scaleY": scaleY * factorY, "offsetX": offsetX * factorX, "offsetY": offsetY * factorY}
				} else {
					capture["cssToOutput"] = nil
					capture["coordinatesAligned"] = false
				}
				result.addImage("当前窗口真实像素（截图期间表单及标记敏感区已隐藏，未加诊断标注）", png)
				// Save only after capture metadata and coverage are attached.
			}
		}
	}
	observation["capture"] = capture
	coverage := uiMap(observation["coverage"])
	if coverage == nil {
		coverage = map[string]any{}
		observation["coverage"] = coverage
	}
	coverage["pixels"] = capture["status"]
	if aligned, ok := capture["coordinatesAligned"].(bool); ok {
		coverage["coordinatesAligned"] = aligned
	}
	health := "healthy"
	if !uiBool(observation, "ready") {
		health = "unknown"
	} else if len(uiMapSlice(observation["issues"])) > 0 || len(uiMapSlice(observation["errors"])) > 0 {
		health = "degraded"
	}
	if includeImage && capture["status"] != "captured" {
		health = "degraded"
	}
	if aligned, ok := capture["coordinatesAligned"].(bool); ok && !aligned {
		health = "degraded"
	}
	observation["health"] = map[string]any{"state": health, "coverage": "partial"}
	if name == OpAppObserve {
		data["capture"] = capture
		data["health"] = observation["health"]
		uiMap(data["frontend"])["state"] = health
	}
	data["elapsedMs"] = time.Since(started).Milliseconds()
	if id := uiArgString(observation, "observationId"); id != "" {
		var png []byte
		if len(result.Images) > 0 {
			png, _ = decodeImage(result.Images[0].Data)
		}
		stored := s.saveObservation(ctx, observation, png)
		data["storage"] = map[string]any{"stored": stored, "imageStored": stored && len(png) > 0, "retentionSec": 3600}
	}
	boundObservationData(data)
	return result, nil
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// observationCoordinatesAligned reports whether semantic node rectangles and
// the lock-time capture observation describe the same coordinate space. The
// frame sequence is intentionally excluded: installing a renderer capture
// lock advances paint frames by design, while mutation/layout changes can
// invalidate node coordinates.
func observationCoordinatesAligned(semantic, locked map[string]any) bool {
	if semantic == nil || locked == nil {
		return false
	}
	if pageEpoch, ok := semantic["pageEpoch"].(string); !ok || pageEpoch == "" || pageEpoch != uiArgString(locked, "pageEpoch") {
		return false
	}
	semanticViewport, lockedViewport := uiMap(semantic["viewport"]), uiMap(locked["viewport"])
	if semanticViewport == nil || lockedViewport == nil {
		return false
	}
	if !sameJSON(semanticViewport, lockedViewport) {
		return false
	}
	semanticSeq, lockedSeq := uiMap(semantic["sequences"]), uiMap(locked["sequences"])
	if semanticSeq == nil || lockedSeq == nil {
		return false
	}
	for _, key := range []string{"mutation", "layout"} {
		left, leftOK := observationNumber(semanticSeq[key])
		right, rightOK := observationNumber(lockedSeq[key])
		if !leftOK || !rightOK || left != right {
			return false
		}
	}
	return true
}

func observationBudget(name string, args map[string]any) (time.Duration, error) {
	timeout, err := jobArgInt(args, "timeoutMs", 1000, 0, 15000)
	if err != nil {
		return 0, err
	}
	// ui.act contains a second wait budget. The outer context must cover that
	// wait as well as the bridge/capture allowance; otherwise timeoutMs on the
	// action's explicit postcondition would be clipped by the historical
	// default (timeoutMs+5s).
	if name == OpUIAct {
		if waitArgs := uiMap(args["wait"]); waitArgs != nil {
			waitTimeout, waitErr := jobArgInt(waitArgs, "timeoutMs", 1000, 0, 15000)
			if waitErr != nil {
				return 0, waitErr
			}
			if waitTimeout > timeout {
				timeout = waitTimeout
			}
		}
	}
	return time.Duration(timeout+5000) * time.Millisecond, nil
}

func observationNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// boundObservationData keeps a single observation useful under MCP/HTTP
// response budgets. The full image is carried as a separate content item.
func boundObservationData(data map[string]any) {
	if data == nil {
		return
	}
	for _, key := range []string{"nodes", "issues", "errors"} {
		if values, ok := data[key].([]any); ok && len(values) > 200 {
			data[key] = values[:200]
		}
	}
	if raw, err := json.Marshal(data); err == nil && len(raw) > uiMaxDataBytes {
		// Preserve the top-level health/capture assertions first. Large semantic
		// lists are diagnostic detail and can be recovered through ui.inspect.
		delete(data, "nodes")
		delete(data, "issues")
		delete(data, "errors")
		data["responseBudget"] = map[string]any{"limited": true, "maxBytes": uiMaxDataBytes, "detail": "semantic lists omitted; inspect a stable target or run ui.diagnose"}
	}
}
func pngConfig(data []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(data)) }

func prepareObservationPixels(frame CaptureFrame, obs, args map[string]any) ([]byte, int, error) {
	pixelProcessingSlot <- struct{}{}
	defer func() { <-pixelProcessingSlot }()
	if len(frame.PNG) > 32<<20 {
		return nil, 0, fmt.Errorf("CAPTURE_TOO_LARGE")
	}
	cfg, e := pngConfig(frame.PNG)
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32<<20 {
		return nil, 0, fmt.Errorf("CAPTURE_INVALID_OR_TOO_LARGE")
	}
	img, e := uiDecodePNG(frame.PNG)
	if e != nil {
		return nil, 0, e
	}
	if img.Bounds().Dx()*img.Bounds().Dy() > 32*1024*1024 {
		return nil, 0, fmt.Errorf("CAPTURE_TOO_LARGE")
	}
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, img.Bounds().Min, draw.Src)
	sx, sy := frame.ScaleX, frame.ScaleY
	if sx <= 0 || sy <= 0 {
		vp := uiMap(obs["viewport"])
		w, h := uiNum(vp["width"]), uiNum(vp["height"])
		if w <= 0 {
			w = uiNum(vp["cssWidth"])
		}
		if h <= 0 {
			h = uiNum(vp["cssHeight"])
		}
		if w <= 0 || h <= 0 {
			return nil, 0, fmt.Errorf("COORDINATE_UNAVAILABLE")
		}
		sx = float64(img.Bounds().Dx()) / w
		sy = float64(img.Bounds().Dy()) / h
	}
	redacted := 0
	regions := uiMapSlice(obs["sensitiveRects"])
	for _, n := range uiMapSlice(obs["nodes"]) {
		if uiBool(n, "sensitive") {
			if r := uiMap(n["rect"]); r != nil {
				regions = append(regions, r)
			}
		}
	}
	for _, r := range regions {
		x, y, w, h := uiNum(r["x"]), uiNum(r["y"]), uiNum(r["w"]), uiNum(r["h"])
		rect := image.Rect(int(math.Floor(x*sx+frame.OffsetX)), int(math.Floor(y*sy+frame.OffsetY)), int(math.Ceil((x+w)*sx+frame.OffsetX)), int(math.Ceil((y+h)*sy+frame.OffsetY))).Intersect(canvas.Bounds())
		if !rect.Empty() {
			draw.Draw(canvas, rect, &image.Uniform{color.RGBA{32, 32, 32, 255}}, image.Point{}, draw.Src)
			redacted++
		}
	}
	maxDimension := uiClampInt(args["maxImageDimension"], 1440, 320, 4096)
	var output image.Image = canvas
	dimension := maxInt(canvas.Bounds().Dx(), canvas.Bounds().Dy())
	if dimension > maxDimension {
		output = uiResampleNearest(canvas, float64(maxDimension)/float64(dimension))
	}
	png, e := uiEncodePNG(output)
	return png, redacted, e
}
func (s *Server) saveObservation(ctx context.Context, data map[string]any, png []byte) bool {
	id := uiArgString(data, "observationId")
	if id == "" {
		return false
	}
	raw, e := json.Marshal(data)
	if e != nil || len(raw) > 512<<10 || len(png) > 16<<20 {
		return false
	}
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	if s.observations == nil {
		s.observations = map[string]storedObservation{}
	}
	s.observations[id] = storedObservation{CallerID(ctx), time.Now().UnixMilli(), clone, append([]byte(nil), png...)}
	for {
		bytes := 0
		oldest := ""
		at := int64(math.MaxInt64)
		for k, r := range s.observations {
			bytes += len(r.PNG)
			if r.At < at {
				at = r.At
				oldest = k
			}
		}
		if len(s.observations) <= 64 && bytes <= 64<<20 && time.Now().UnixMilli()-at <= 3600000 {
			break
		}
		if oldest == "" {
			break
		}
		delete(s.observations, oldest)
	}
	_, ok := s.observations[id]
	return ok
}
func (s *Server) loadObservation(ctx context.Context, id string) (storedObservation, error) {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	r, ok := s.observations[id]
	if !ok || r.Owner != CallerID(ctx) || time.Now().UnixMilli()-r.At > 3600000 {
		return r, fmt.Errorf("%w: observation不存在、过期或不属于当前身份", ErrNotFound)
	}
	raw, _ := json.Marshal(r.Data)
	r.Data = nil
	_ = json.Unmarshal(raw, &r.Data)
	r.PNG = append([]byte(nil), r.PNG...)
	return r, nil
}
func (s *Server) compareObservations(ctx context.Context, args map[string]any) (*UIResult, error) {
	a, e := s.loadObservation(ctx, uiArgString(args, "baselineId"))
	if e != nil {
		return nil, e
	}
	b, e := s.loadObservation(ctx, uiArgString(args, "currentId"))
	if e != nil {
		return nil, e
	}
	result := uiResult(map[string]any{"baselineId": args["baselineId"], "currentId": args["currentId"], "comparable": false})
	for _, key := range []string{"viewport", "windowId", "build", "appearance"} {
		x, _ := json.Marshal(a.Data[key])
		y, _ := json.Marshal(b.Data[key])
		if a.Data[key] == nil || b.Data[key] == nil || string(x) != string(y) {
			result.Data["reasonCode"] = "ENVIRONMENT_MISMATCH"
			result.Data["mismatch"] = key
			return result, nil
		}
	}
	ac, bc := uiMap(a.Data["capture"]), uiMap(b.Data["capture"])
	for _, key := range []string{"provider", "surface", "outputPixelSize", "cssToOutput", "redactions", "redactionMode"} {
		if ac[key] == nil || bc[key] == nil || !sameJSON(ac[key], bc[key]) {
			result.Data["reasonCode"] = "CAPTURE_ENVIRONMENT_MISMATCH"
			result.Data["mismatch"] = "capture." + key
			return result, nil
		}
	}
	if !sameJSON(a.Data["sensitiveRects"], b.Data["sensitiveRects"]) {
		result.Data["reasonCode"] = "REDACTION_GEOMETRY_MISMATCH"
		return result, nil
	}
	if ac["consistency"] != "stable-window" || bc["consistency"] != "stable-window" {
		result.Data["reasonCode"] = "CAPTURE_NOT_STABLE"
		return result, nil
	}
	fontsA, fontsB := uiMap(uiMap(a.Data["runtime"])["fonts"]), uiMap(uiMap(b.Data["runtime"])["fonts"])
	if fontsA == nil || fontsB == nil || !sameJSON(fontIdentity(fontsA), fontIdentity(fontsB)) {
		result.Data["reasonCode"] = "FONT_ENVIRONMENT_MISMATCH"
		return result, nil
	}
	if len(a.PNG) == 0 || len(b.PNG) == 0 {
		result.Data["reasonCode"] = "PIXELS_UNAVAILABLE"
		return result, nil
	}
	ai, e := uiDecodePNG(a.PNG)
	if e != nil {
		return nil, e
	}
	bi, e := uiDecodePNG(b.PNG)
	if e != nil {
		return nil, e
	}
	if ai.Bounds() != bi.Bounds() {
		result.Data["reasonCode"] = "IMAGE_SIZE_MISMATCH"
		return result, nil
	}
	threshold, e := jobArgInt(args, "threshold", 12, 0, 255)
	if e != nil {
		return nil, e
	}
	diff := uiPixelDiff(ai, bi, uiDiffTile, threshold)
	result.Data["comparable"] = true
	result.Data["pixels"] = diff
	result.Data["semantics"] = compareSemantics(a.Data, b.Data)
	result.Data["note"] = "像素变化不是自动的UI错误判定；动态内容尚未屏蔽。"
	return result, nil
}

func compareSemantics(a, b map[string]any) map[string]any {
	if a["pageEpoch"] != b["pageEpoch"] {
		return map[string]any{"comparable": false, "reasonCode": "PAGE_EPOCH_MISMATCH"}
	}
	oldNodes, newNodes := uiMapSlice(a["nodes"]), uiMapSlice(b["nodes"])
	oldIndex, newIndex := map[string]map[string]any{}, map[string]map[string]any{}
	for _, n := range oldNodes {
		if ref := uiArgString(n, "ref"); ref != "" {
			oldIndex[ref] = n
		}
	}
	for _, n := range newNodes {
		if ref := uiArgString(n, "ref"); ref != "" {
			newIndex[ref] = n
		}
	}
	added, removed, changed := []any{}, []any{}, []any{}
	for ref, n := range newIndex {
		old, ok := oldIndex[ref]
		if !ok {
			if len(added) < 50 {
				added = append(added, map[string]any{"ref": ref, "role": n["role"], "name": n["name"]})
			}
			continue
		}
		facts := map[string]any{}
		for _, key := range []string{"role", "name", "automationId", "rect", "visible", "actionable", "disabled", "inert"} {
			if !sameJSON(old[key], n[key]) {
				facts[key] = map[string]any{"before": old[key], "after": n[key]}
			}
		}
		if len(facts) > 0 && len(changed) < 50 {
			changed = append(changed, map[string]any{"ref": ref, "changes": facts})
		}
	}
	for ref, n := range oldIndex {
		if _, ok := newIndex[ref]; !ok && len(removed) < 50 {
			removed = append(removed, map[string]any{"ref": ref, "role": n["role"], "name": n["name"]})
		}
	}
	sort.Slice(added, func(i, j int) bool { return uiArgString(uiMap(added[i]), "ref") < uiArgString(uiMap(added[j]), "ref") })
	sort.Slice(removed, func(i, j int) bool {
		return uiArgString(uiMap(removed[i]), "ref") < uiArgString(uiMap(removed[j]), "ref")
	})
	sort.Slice(changed, func(i, j int) bool {
		return uiArgString(uiMap(changed[i]), "ref") < uiArgString(uiMap(changed[j]), "ref")
	})
	coverageA, coverageB := uiMap(a["coverage"]), uiMap(b["coverage"])
	complete := coverageA["truncated"] == false && coverageB["truncated"] == false
	return map[string]any{"comparable": true, "coverage": map[string]any{"complete": complete, "baselineCount": len(oldNodes), "currentCount": len(newNodes), "truncated": !complete}, "added": added, "removed": removed, "changed": changed}
}

func fontIdentity(fonts map[string]any) map[string]any {
	return map[string]any{"supported": fonts["supported"], "status": fonts["status"], "faces": fonts["faces"]}
}
func registerObservationRoutes(mux *http.ServeMux, s *Server) {
	routes := map[string]string{"app/observe": OpAppObserve, "app/runtime-capabilities": OpRuntimeCapabilities, "app/diagnostics": OpDiagnostics, "ui/observe": OpUIObserve, "ui/inspect": OpUIInspect, "ui/diagnose": OpUIDiagnose, "ui/wait": OpUIWait, "ui/act": OpUIAct, "ui/compare": OpUICompare, "verify/run": OpVerifyRun, "verify/get": OpVerifyGet, "targets/probe": OpTargetsProbe}
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, path := range keys {
		name := routes[path]
		handler := func(w http.ResponseWriter, r *http.Request) {
			raw, e := s.buildArgs(r, "")
			if e != nil {
				writeJSON(w, statusFor(e), map[string]any{"error": e.Error()})
				return
			}
			args := map[string]any{}
			if len(raw) > 0 {
				if e = json.Unmarshal(raw, &args); e != nil {
					writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
					return
				}
			}
			if entry, ok := toolEntryByName(name); ok {
				if e = validateToolSchema(entry.InputSchema, args, "arguments"); e != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": e.Error()})
					return
				}
			}
			content, _, e := s.mcpCallToolAudited(r.Context(), name, args)
			if e != nil {
				writeJSON(w, statusFor(e), map[string]any{"error": e.Error()})
				return
			}
			out := map[string]any{}
			images := []any{}
			for _, c := range content {
				m := uiMap(c)
				if m["type"] == "text" {
					_ = json.Unmarshal([]byte(uiArgString(m, "text")), &out)
				} else if m["type"] == "image" {
					images = append(images, m)
				}
			}
			if len(images) > 0 {
				out["imageContent"] = images
			}
			writeJSON(w, 200, out)
		}
		mux.HandleFunc("POST /v1/"+path, s.named(name, handler))
		if name != OpUIAct && name != OpVerifyRun {
			mux.HandleFunc("GET /v1/"+path, s.named(name, handler))
		}
	}
}

// decodeImage is kept local to avoid embedding pixel bytes in JSON metadata.
func decodeImage(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
