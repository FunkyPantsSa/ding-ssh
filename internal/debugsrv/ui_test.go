package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ding-ssh/internal/logx"
)

// UI 观测 / 实验域的 Go 侧测试。
//
// 覆盖四类最容易悄悄出错、又最贵的逻辑（不依赖 Wails / WebView）：
//  1. 截图坐标换算（dpr / 界面缩放 / 标题栏偏移）与裁剪；
//  2. 像素差异（相同 → 0；一块变红 → 区域定位正确）与 diff 图；
//  3. 快照名 / 路径校验（防目录穿越）与快照落盘、ui.diff 读盘；
//  4. 能力位注册、ui.assert 判定、截断与体积上限、HTTP 路由的能力校验。
//
// 「向前端要数据」全部通过 uiFrontend 替身（stubUI）注入，因此这些用例可以在
// 没有 Wails 的环境（CI）里跑。

// ---- 替身前端 ----

// stubUI 按 op 返回预设数据，并记录被调用的 op 与参数。
type stubUI struct {
	replies map[string]any
	errs    map[string]error
	calls   []string
	args    []map[string]any
}

func (s *stubUI) CallUI(_ context.Context, op string, args map[string]any) (any, error) {
	s.calls = append(s.calls, op)
	s.args = append(s.args, args)
	if s.errs != nil {
		if err, bad := s.errs[op]; bad {
			return nil, err
		}
	}
	if v, ok := s.replies[op]; ok {
		return v, nil
	}
	return map[string]any{"op": op}, nil
}

func (s *stubUI) called(op string) bool {
	for _, c := range s.calls {
		if c == op {
			return true
		}
	}
	return false
}

// ensureUIRegistrations 见 registry_test.go（M9 收尾：跨域注册助手统一归位到那里）。

// ---- 1. 坐标换算与裁剪 ----

// 换算依据：截图是窗口**外框**的物理像素，前端给的是视口 CSS 坐标，
// 因此需要「标题栏 / 边框偏移 + 每 CSS px 的截图 px 数（自动标定）」两步。
func TestUIScaleCalcAndCrop(t *testing.T) {
	cases := []struct {
		name         string
		imgW, imgH   int
		geo          map[string]any
		rect         uiRect
		wantScaleX   float64
		wantScaleY   float64
		wantChromeX  float64
		wantChromeY  float64
		wantDrift    bool
		wantRect     image.Rectangle
		wantUIScale  float64
		wantEmptyErr bool
	}{
		{
			// 1280x800 窗口、25% 显示缩放：截图 1600x1000 px，标题栏 31 CSS px（=800-769）
			name: "dpr1.25-带标题栏",
			imgW: 1600, imgH: 1000,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.25, "uiScale": 1.0},
				"outer":    map[string]any{"w": 1280.0, "h": 800.0},
				"inner":    map[string]any{"w": 1280.0, "h": 769.0},
			},
			rect:        uiRect{X: 10, Y: 20, W: 100, H: 50},
			wantScaleX:  1.25,
			wantScaleY:  1.25,
			wantChromeY: 31,
			wantDrift:   false,
			// x0=round(10*1.25)=13, y0=round((20+31)*1.25)=64, x1=round(110*1.25)=138, y1=round((70+31)*1.25)=126
			wantRect:    image.Rect(13, 64, 138, 126),
			wantUIScale: 1,
		},
		{
			// 界面缩放 120%（前端给的是含 zoom 的可视坐标，因此这里不应再乘 1.2）
			name: "界面缩放120%",
			imgW: 1200, imgH: 800,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.0, "uiScale": 120.0},
				"outer":    map[string]any{"w": 1200.0, "h": 800.0},
				"inner":    map[string]any{"w": 1200.0, "h": 800.0},
			},
			rect:        uiRect{X: 0, Y: 0, W: 600, H: 400},
			wantScaleX:  1,
			wantScaleY:  1,
			wantChromeY: 0,
			wantDrift:   false,
			wantRect:    image.Rect(0, 0, 600, 400),
			wantUIScale: 1.2,
		},
		{
			// 自动标定比例与 devicePixelRatio 不一致 → Drift=true（提示换算可能有偏差）
			name: "比例与dpr不一致",
			imgW: 1280, imgH: 800,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.5, "uiScale": 1.0},
				"outer":    map[string]any{"w": 1280.0, "h": 800.0},
				"inner":    map[string]any{"w": 1280.0, "h": 800.0},
			},
			rect:        uiRect{X: 0, Y: 0, W: 100, H: 100},
			wantScaleX:  1,
			wantScaleY:  1,
			wantChromeY: 0,
			wantDrift:   true,
			wantRect:    image.Rect(0, 0, 100, 100),
			wantUIScale: 1,
		},
		{
			// 超出截图范围：交集裁剪（右下角被截断）
			name: "超出范围裁剪",
			imgW: 100, imgH: 100,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.0, "uiScale": 1.0},
				"outer":    map[string]any{"w": 100.0, "h": 100.0},
				"inner":    map[string]any{"w": 100.0, "h": 100.0},
			},
			rect:        uiRect{X: 80, Y: 80, W: 100, H: 100},
			wantScaleX:  1,
			wantScaleY:  1,
			wantChromeY: 0,
			wantRect:    image.Rect(80, 80, 100, 100),
			wantUIScale: 1,
		},
		{
			// 完全在窗口外：空矩形（调用方会返回中文错误）
			name: "完全越界",
			imgW: 100, imgH: 100,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.0, "uiScale": 1.0},
				"outer":    map[string]any{"w": 100.0, "h": 100.0},
				"inner":    map[string]any{"w": 100.0, "h": 100.0},
			},
			rect:         uiRect{X: 500, Y: 500, W: 100, H: 100},
			wantScaleX:   1,
			wantScaleY:   1,
			wantUIScale:  1,
			wantEmptyErr: true,
		},
		{
			// 活体实测得到的真实形态（WebView2）：window.outerWidth/Height == innerWidth/Height
			// （都等于内容区），截图却是含标题栏与边框的 1280x800。
			// 此时用「截图 ÷ dpr − 视口」反推：单侧边框 = (1280−1264)/2 = 8，标题栏 = 800−761−8 = 31。
			name: "WebView2-outer等于inner-用截图反推",
			imgW: 1280, imgH: 800,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.0, "uiScale": 1.0, "width": 1264.0, "height": 761.0},
				"outer":    map[string]any{"w": 1264.0, "h": 761.0},
				"inner":    map[string]any{"w": 1264.0, "h": 761.0},
			},
			rect:        uiRect{X: 194, Y: 43, W: 1070, H: 691},
			wantScaleX:  1,
			wantScaleY:  1,
			wantChromeX: 8,
			wantChromeY: 31,
			wantDrift:   false,
			// x0=round(194+8)=202, y0=round(43+31)=74, x1=202+1070=1272, y1=74+691=765
			wantRect:    image.Rect(202, 74, 1272, 765),
			wantUIScale: 1,
		},
		{
			// 反推结果明显不合理（截图远大于内容区且 dpr 不合理）→ 退回 0 并标记 Drift
			name: "反推不合理时退回0",
			imgW: 4000, imgH: 3000,
			geo: map[string]any{
				"viewport": map[string]any{"dpr": 1.0, "uiScale": 1.0, "width": 100.0, "height": 100.0},
				"outer":    map[string]any{"w": 100.0, "h": 100.0},
				"inner":    map[string]any{"w": 100.0, "h": 100.0},
			},
			rect:        uiRect{X: 0, Y: 0, W: 50, H: 50},
			wantScaleX:  40, // 4000/100
			wantScaleY:  30, // 3000/100
			wantChromeX: 0,
			wantChromeY: 0,
			wantDrift:   true,
			wantRect:    image.Rect(0, 0, 2000, 1500),
			wantUIScale: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calc := uiComputeScale(tc.imgW, tc.imgH, tc.geo)
			if !almostEqual(calc.ScaleX, tc.wantScaleX) || !almostEqual(calc.ScaleY, tc.wantScaleY) {
				t.Fatalf("自动标定比例 = (%.4f, %.4f)，期望 (%.4f, %.4f)", calc.ScaleX, calc.ScaleY, tc.wantScaleX, tc.wantScaleY)
			}
			if !almostEqual(calc.ChromeX, tc.wantChromeX) {
				t.Fatalf("边框偏移 = %.1f，期望 %.1f", calc.ChromeX, tc.wantChromeX)
			}
			if !almostEqual(calc.ChromeY, tc.wantChromeY) {
				t.Fatalf("标题栏偏移 = %.1f，期望 %.1f", calc.ChromeY, tc.wantChromeY)
			}
			if !almostEqual(calc.UIScale, tc.wantUIScale) {
				t.Fatalf("界面缩放 = %.2f，期望 %.2f（应把百分比 120 归一成 1.2）", calc.UIScale, tc.wantUIScale)
			}
			if calc.Drift != tc.wantDrift {
				t.Fatalf("Drift = %v，期望 %v", calc.Drift, tc.wantDrift)
			}
			got := calc.imageRect(tc.rect)
			if tc.wantEmptyErr {
				if !got.Empty() {
					t.Fatalf("完全越界的矩形应被裁成空，实际 %v", got)
				}
			} else if got != tc.wantRect {
				t.Fatalf("imageRect = %v，期望 %v（依据：%s）", got, tc.wantRect, calc.basis())
			}
			if !strings.Contains(calc.basis(), "换算依据") {
				t.Fatalf("basis 文案应说明换算依据：%s", calc.basis())
			}
		})
	}
}

// 裁剪出的图片必须是「原图的对应区域」，不能差一行一列。
func TestUICropImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{R: 10, G: 20, B: 30, A: 255}), image.Point{}, draw.Src)
	// 在 (2,3)-(4,5) 涂红
	for y := 3; y < 5; y++ {
		for x := 2; x < 4; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	out := uiCropImage(src, image.Rect(2, 3, 4, 5))
	if out.Bounds().Dx() != 2 || out.Bounds().Dy() != 2 {
		t.Fatalf("裁剪尺寸 = %dx%d，期望 2x2", out.Bounds().Dx(), out.Bounds().Dy())
	}
	r, g, b, _ := uiPixelAt(out, 0, 0)
	if r != 255 || g != 0 || b != 0 {
		t.Fatalf("裁剪结果首像素应为红色，实际 (%d,%d,%d)", r, g, b)
	}
	// 越界裁剪：交集后为空
	if !uiCropImage(src, image.Rect(100, 100, 110, 110)).Bounds().Empty() {
		t.Fatalf("越界裁剪应返回空图")
	}
}

// 最近邻缩放：尺寸换算正确，且不改变颜色分布（避免差异误报）。
func TestUIResampleNearest(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{R: 200, G: 100, B: 50, A: 255}), image.Point{}, draw.Src)
	out := uiResampleNearest(src, 0.5)
	if out.Bounds().Dx() != 5 || out.Bounds().Dy() != 5 {
		t.Fatalf("缩放后尺寸 = %dx%d，期望 5x5", out.Bounds().Dx(), out.Bounds().Dy())
	}
	r, g, b, _ := uiPixelAt(out, 1, 1)
	if r != 200 || g != 100 || b != 50 {
		t.Fatalf("最近邻缩放不应改变颜色，实际 (%d,%d,%d)", r, g, b)
	}
}

// ---- 2. 像素差异 ----

func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func TestUIPixelDiff(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.RGBA{R: 255, A: 255}
	a := solidImage(64, 64, white)

	// 完全相同 → changedRatio = 0，无区域
	same := solidImage(64, 64, white)
	res := uiPixelDiff(a, same, uiDiffTile, uiDiffDefThresh)
	if res.changedRatio != 0 || res.changedPixels != 0 || res.changedTiles != 0 {
		t.Fatalf("相同图片的差异应为 0，实际 ratio=%v pixels=%d tiles=%d", res.changedRatio, res.changedPixels, res.changedTiles)
	}
	if regions := uiMergeTiles(res.grid, res.tilesX, res.tilesY, uiDiffTile); len(regions) != 0 {
		t.Fatalf("相同图片不应有差异区域：%v", regions)
	}

	// 一块变红：(32,0)-(48,16) = 一个 16x16 分块（网格 4x4 的 (2,0)）
	b := solidImage(64, 64, white)
	for y := 0; y < 16; y++ {
		for x := 32; x < 48; x++ {
			b.SetRGBA(x, y, red)
		}
	}
	res = uiPixelDiff(a, b, uiDiffTile, uiDiffDefThresh)
	if res.changedPixels != 256 {
		t.Fatalf("变化像素数 = %d，期望 256", res.changedPixels)
	}
	if !almostEqual(res.changedRatio, 256.0/4096.0) {
		t.Fatalf("changedRatio = %v，期望 %v", res.changedRatio, 256.0/4096.0)
	}
	regions := uiMergeTiles(res.grid, res.tilesX, res.tilesY, uiDiffTile)
	if len(regions) != 1 {
		t.Fatalf("应合并成 1 个区域，实际 %d：%v", len(regions), regions)
	}
	if regions[0].X != 32 || regions[0].Y != 0 || regions[0].W != 16 || regions[0].H != 16 {
		t.Fatalf("区域定位错误：%+v（期望 x=32 y=0 w=16 h=16）", regions[0])
	}
	if !almostEqual(regions[0].ChangedRatio, 1) {
		t.Fatalf("该区域应完全变化，实际 ratio=%v", regions[0].ChangedRatio)
	}

	// 相邻两块的合并（(0,0) 与 (1,0)）
	c := solidImage(64, 64, white)
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			c.SetRGBA(x, y, red)
		}
	}
	merged := uiMergeTiles(uiPixelDiff(a, c, uiDiffTile, uiDiffDefThresh).grid, 4, 4, uiDiffTile)
	if len(merged) != 1 {
		t.Fatalf("相邻变化块应合并成 1 个区域，实际 %d：%v", len(merged), merged)
	}
	if merged[0].W != 32 || merged[0].H != 16 {
		t.Fatalf("合并后的包围盒 = %dx%d，期望 32x16", merged[0].W, merged[0].H)
	}

	// 阈值：把阈值提到 255 时任何变化都不算（通道差最大 255，用 <= 判定）
	if got := uiPixelDiff(a, b, uiDiffTile, 255); got.changedPixels != 0 {
		t.Fatalf("阈值 255 时不应有变化像素，实际 %d", got.changedPixels)
	}

	// diff 图：变化块被叠上半透明红，未变化区域保持原色
	diff := uiOverlayChanged(a, res.grid, res.tilesX, res.tilesY, uiDiffTile, 110)
	dr, dg, db, _ := uiPixelAt(diff, 33, 5)
	if dr != 255 || dg > 200 || db > 200 {
		t.Fatalf("diff 图在变化块内应偏红，实际 (%d,%d,%d)", dr, dg, db)
	}
	ur, ug, ub, _ := uiPixelAt(diff, 5, 40)
	if ur != 255 || ug != 255 || ub != 255 {
		t.Fatalf("diff 图未变化区域应保持原色，实际 (%d,%d,%d)", ur, ug, ub)
	}
}

// ---- 3. 快照名 / 路径校验 ----

func TestUISnapshotNameAndPaths(t *testing.T) {
	dir := t.TempDir()
	logx.SetLogDirForTest(dir)
	t.Cleanup(func() { logx.SetLogDirForTest("") })

	valid := []string{"base", "after-2", "v1.2.3", "abc_123"}
	for _, name := range valid {
		pngPath, metaPath, err := uiSnapshotPaths(name)
		if err != nil {
			t.Fatalf("合法快照名 %q 应通过：%v", name, err)
		}
		wantDir := filepath.Join(dir, uiSnapshotDirName)
		if filepath.Dir(pngPath) != wantDir || filepath.Dir(metaPath) != wantDir {
			t.Fatalf("快照路径应落在 %s，实际 %s / %s", wantDir, pngPath, metaPath)
		}
		if !strings.HasSuffix(pngPath, name+".png") || !strings.HasSuffix(metaPath, name+".json") {
			t.Fatalf("快照路径命名不对：%s / %s", pngPath, metaPath)
		}
	}

	invalid := []string{
		"", "  ", "../evil", "..", "a/b", `a\b`, "a b", ".hidden", "-dash",
		strings.Repeat("a", 65), `C:\temp\x`, "a:b", "a/../../b",
	}
	for _, name := range invalid {
		pngPath, metaPath, err := uiSnapshotPaths(name)
		if err == nil {
			t.Fatalf("非法快照名 %q 不应通过（得到 %s / %s）", name, pngPath, metaPath)
		}
		if !strings.Contains(err.Error(), "快照名") && !strings.Contains(err.Error(), "缺少快照名") {
			t.Fatalf("错误文案应说明快照名问题，实际：%v", err)
		}
	}

	// 目录穿越：即使名字里带 ..，拼接后的路径也必须在快照目录内
	if _, _, err := uiSnapshotPaths("a..b"); err != nil {
		t.Fatalf("a..b 只是普通名字（不含路径分隔符），应通过：%v", err)
	} else {
		p, _, _ := uiSnapshotPaths("a..b")
		if filepath.Dir(p) != filepath.Join(dir, uiSnapshotDirName) {
			t.Fatalf("路径越界：%s", p)
		}
	}

	// 读取快照：不存在时给出中文错误（含「不存在」与路径提示）
	if _, err := uiLoadSnapshot("nope"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("不存在的快照应返回中文错误，实际：%v", err)
	}
}

// ---- 4. ui.diff 端到端（读盘 + 像素 + 指纹 + revision 提示）----

func writeFakeSnapshot(t *testing.T, name string, png []byte, revision any, fingerprint []map[string]any) {
	t.Helper()
	pngPath, metaPath, err := uiSnapshotPaths(name)
	if err != nil {
		t.Fatalf("快照路径无效：%v", err)
	}
	if err := uiWriteFile(pngPath, png); err != nil {
		t.Fatalf("写入快照 PNG 失败：%v", err)
	}
	meta := uiSnapshotMeta{
		Name: name, TS: time.Now().UnixMilli(), Revision: revision,
		Viewport:    map[string]any{"width": 64, "height": 64, "dpr": 1.0},
		ImageSize:   map[string]int{"w": 64, "h": 64},
		PNGPath:     pngPath,
		Fingerprint: fingerprint,
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("序列化快照元信息失败：%v", err)
	}
	if err := uiWriteFile(metaPath, b); err != nil {
		t.Fatalf("写入快照元信息失败：%v", err)
	}
}

func TestUIDiffEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logx.SetLogDirForTest(dir)
	t.Cleanup(func() { logx.SetLogDirForTest("") })

	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.RGBA{R: 255, A: 255}
	basePNG, err := uiEncodePNG(solidImage(64, 64, white))
	if err != nil {
		t.Fatalf("编码基准图失败：%v", err)
	}
	changed := solidImage(64, 64, white)
	for y := 0; y < 16; y++ {
		for x := 32; x < 48; x++ {
			changed.SetRGBA(x, y, red)
		}
	}
	afterPNG, err := uiEncodePNG(changed)
	if err != nil {
		t.Fatalf("编码对比图失败：%v", err)
	}

	writeFakeSnapshot(t, "base", basePNG, 1.0, []map[string]any{
		{"selectorPath": "#app > div:nth-of-type(1)", "tag": "div", "rect": map[string]any{"x": 0, "y": 0, "w": 64, "h": 64}, "classes": []any{"shell"}, "text": "", "zIndex": "auto", "visible": true,
			"styles": map[string]any{"color": "rgb(255, 255, 255)", "border-radius": "4px"}},
		{"selectorPath": "#app > div:nth-of-type(2)", "tag": "div", "rect": map[string]any{"x": 0, "y": 0, "w": 10, "h": 10}, "classes": []any{"gone"}, "text": "移除", "zIndex": "auto", "visible": true},
	})
	writeFakeSnapshot(t, "after", afterPNG, 2.0, []map[string]any{
		{"selectorPath": "#app > div:nth-of-type(1)", "tag": "div", "rect": map[string]any{"x": 0, "y": 0, "w": 64, "h": 64}, "classes": []any{"shell", "wide"}, "text": "改了文本", "zIndex": "auto", "visible": true,
			"styles": map[string]any{"color": "rgb(255, 255, 255)", "border-radius": "16px"}},
		{"selectorPath": "#app > div:nth-of-type(3)", "tag": "div", "rect": map[string]any{"x": 0, "y": 0, "w": 5, "h": 5}, "classes": []any{"new"}, "text": "新增", "zIndex": "10", "visible": true},
	})

	s := New(Options{Handler: &fakeHandler{}})
	res, err := s.uiDiff(map[string]any{"a": "base", "b": "after"})
	if err != nil {
		t.Fatalf("ui.diff 失败：%v", err)
	}
	if !almostEqual(uiNum(res.Data["changedRatio"]), 256.0/4096.0) {
		t.Fatalf("changedRatio = %v，期望 %v", res.Data["changedRatio"], 256.0/4096.0)
	}
	regions, _ := res.Data["regions"].([]any)
	if len(regions) != 1 {
		t.Fatalf("应定位到 1 个变化区域，实际 %d：%v", len(regions), regions)
	}
	first, _ := regions[0].(uiDiffRegion)
	if first.X != 32 || first.Y != 0 || first.W != 16 || first.H != 16 {
		t.Fatalf("区域定位错误：%+v", first)
	}
	if len(res.Images) != 1 || res.Images[0].MimeType != "image/png" || res.Images[0].Bytes == 0 {
		t.Fatalf("ui.diff 应返回 1 张 diff 图，实际 %+v", res.Images)
	}
	diffPath, _ := res.Data["diffPngPath"].(string)
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("diff 图应落盘（%s）：%v", diffPath, err)
	}
	if filepath.Dir(diffPath) != filepath.Join(dir, uiSnapshotDirName) {
		t.Fatalf("diff 图应落在快照目录：%s", diffPath)
	}
	// revision 不同 → 显式提示期间发生过重载
	if _, has := res.Data["reloadWarning"]; !has {
		t.Fatalf("跨 revision 的 diff 应提示「期间发生过重载」：%v", res.Data)
	}
	fp, _ := res.Data["fingerprint"].(map[string]any)
	if uiNum(fp["addedCount"]) != 1 || uiNum(fp["removedCount"]) != 1 || uiNum(fp["changedCount"]) != 1 {
		t.Fatalf("指纹差异统计不对：added=%v removed=%v changed=%v", fp["addedCount"], fp["removedCount"], fp["changedCount"])
	}
	changedItem, _ := fp["changed"].([]any)[0].(map[string]any)
	fields, _ := changedItem["fields"].([]any)
	names := map[string]bool{}
	var stylesEntry map[string]any
	for _, f := range fields {
		if m, ok := f.(map[string]any); ok {
			names[uiArgString(m, "name")] = true
			if uiArgString(m, "name") == "styles" {
				stylesEntry = m
			}
		}
	}
	if !names["classes"] || !names["text"] {
		t.Fatalf("变化字段应含 classes 与 text（rect 相同不应上报）：%v", fields)
	}
	if names["rect"] {
		t.Fatalf("rect 未变化时不应出现在变化字段里：%v", fields)
	}
	// M9：styles 细化到属性级 —— 只报真正变化的属性（color 没变，不许出现）。
	if stylesEntry == nil {
		t.Fatalf("border-radius 变化时应出现 styles 字段：%v", fields)
	}
	if _, hasFromTo := stylesEntry["from"]; hasFromTo {
		t.Fatalf("styles 字段不应再整份 from/to 上报：%v", stylesEntry)
	}
	props, _ := stylesEntry["styles"].(map[string]any)
	if len(props) != 1 {
		t.Fatalf("styles 只应报真正变化的属性（1 个），实际 %v", props)
	}
	radius, _ := props["border-radius"].(map[string]any)
	if radius == nil || radius["from"] != "4px" || radius["to"] != "16px" {
		t.Fatalf("border-radius 的 from/to 不对：%v", props)
	}
	if _, leaked := props["color"]; leaked {
		t.Fatalf("未变化的 color 不应出现在 styles 里：%v", props)
	}

	// MCP 内容：文本 + 图片
	content, err := uiMCPContent(res)
	if err != nil {
		t.Fatalf("组装 MCP 内容失败：%v", err)
	}
	if len(content) != 2 {
		t.Fatalf("MCP 内容应为「文本 + 图片」两项，实际 %d", len(content))
	}
	textItem, _ := content[0].(map[string]any)
	if !strings.Contains(textItem["text"].(string), "images") {
		t.Fatalf("文本部分应说明图片内容：%s", textItem["text"])
	}
	imgItem, _ := content[1].(map[string]any)
	if imgItem["type"] != "image" || uiArgString(imgItem, "mimeType") != "image/png" {
		t.Fatalf("第二项应是 image 内容：%v", imgItem)
	}

	// 尺寸不同 → 按最小公共区域裁剪并说明
	smallPNG, _ := uiEncodePNG(solidImage(32, 32, white))
	writeFakeSnapshot(t, "small", smallPNG, 3.0, nil)
	res2, err := s.uiDiff(map[string]any{"a": "small", "b": "base"})
	if err != nil {
		t.Fatalf("尺寸不同的 diff 应成功（按公共区域裁剪）：%v", err)
	}
	if !strings.Contains(uiArgString(res2.Data, "sizeMismatch"), "公共区域") {
		t.Fatalf("尺寸不同应给出说明：%v", res2.Data)
	}
	if uiNum(res2.Data["totalPixels"]) != 32*32 {
		t.Fatalf("公共区域应为 32x32，实际 totalPixels=%v", res2.Data["totalPixels"])
	}

	// 不存在的快照 → 中文错误
	if _, err := s.uiDiff(map[string]any{"a": "base", "b": "missing"}); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺失快照应返回中文错误，实际：%v", err)
	}
	if _, err := s.uiDiff(map[string]any{"a": "base"}); err == nil {
		t.Fatalf("缺少 b 应报错")
	}
}

// M9：styles 指纹的属性级 diff（ui.diff 的 changed[].fields[] 里 styles 的形状）。
func TestUIStyleDiffPropertyLevel(t *testing.T) {
	// 只有部分属性变化 → 只报变化的那些
	a := map[string]any{"color": "rgb(1, 2, 3)", "border-radius": "4px", "display": "block"}
	b := map[string]any{"color": "rgb(1, 2, 3)", "border-radius": "16px", "display": "block"}
	got := uiStyleDiff(a, b)
	if len(got) != 1 {
		t.Fatalf("应只报 1 个变化属性，实际 %v", got)
	}
	radius, _ := got["border-radius"].(map[string]any)
	if radius == nil || radius["from"] != "4px" || radius["to"] != "16px" {
		t.Fatalf("border-radius 的 from/to 不对：%v", got)
	}

	// 没有任何属性变化 → 空（调用方据此不输出 styles 条目）
	if out := uiStyleDiff(a, map[string]any{"color": "rgb(1, 2, 3)", "border-radius": "4px", "display": "block"}); len(out) != 0 {
		t.Fatalf("属性全等时不应有任何条目：%v", out)
	}

	// 新增 / 删除的属性也要报（缺失一侧为 nil）
	out := uiStyleDiff(map[string]any{"color": "red"}, map[string]any{"opacity": "0.5"})
	if len(out) != 2 {
		t.Fatalf("新增与删除的属性都应上报，实际 %v", out)
	}
	if v, _ := out["color"].(map[string]any); v == nil || v["from"] != "red" || v["to"] != nil {
		t.Fatalf("被删除的属性应有 from 且 to=nil：%v", out)
	}
	if v, _ := out["opacity"].(map[string]any); v == nil || v["from"] != nil || v["to"] != "0.5" {
		t.Fatalf("新增的属性应有 from=nil：%v", out)
	}

	// 值不是对象（异常指纹）时不能慌：报不出属性就当没有变化
	if out := uiStyleDiff("block", "flex"); len(out) != 0 {
		t.Fatalf("非对象 styles 不应产生属性条目：%v", out)
	}
	if out := uiStyleDiff(nil, nil); len(out) != 0 {
		t.Fatalf("nil styles 不应产生属性条目：%v", out)
	}

	// 指纹差异组装：styles 未变化时不应出现 styles 条目
	fp := uiFingerprintDiff(
		[]map[string]any{{"selectorPath": "#a", "tag": "div", "styles": map[string]any{"color": "red"}}},
		[]map[string]any{{"selectorPath": "#a", "tag": "div", "styles": map[string]any{"color": "red"}}},
	)
	if uiNum(fp["changedCount"]) != 0 {
		t.Fatalf("styles 相同时不应报变化：%v", fp["changed"])
	}
}

// ---- 5. 工具能力位注册 ----

func TestUIToolCapsRegistered(t *testing.T) {
	ensureUIRegistrations(t)
	// 不写死数量：注册表里 ui.* 的登记数必须等于 uiToolNames 的长度
	//（工具清单加了但忘记登记能力位 → 这条会失败）。
	registeredUI := 0
	for _, name := range AllRegisteredToolNames() {
		if strings.HasPrefix(name, "ui.") {
			registeredUI++
		}
	}
	if registeredUI != len(uiToolNames) {
		t.Fatalf("注册表里 ui.* 工具 %d 个，工具清单 %d 个（登记与清单不一致）", registeredUI, len(uiToolNames))
	}
	for _, name := range uiToolNames {
		caps, known := ToolCapabilities(name)
		if !known {
			t.Fatalf("工具 %s 未登记能力位", name)
		}
		want := CapRead
		if uiWriteTools[name] {
			want = CapUIWrite
		}
		if len(caps) != 1 || caps[0] != want {
			t.Fatalf("工具 %s 的能力位 = %v，期望 [%s]", name, caps, want)
		}
		if _, need := ToolConfirmAction(name); need {
			t.Fatalf("可逆 / 高频的 ui.* 工具不应要求两段式确认：%s", name)
		}
	}
	// 写类工具的描述里必须出现「界面写入（ui.write）」与「不需要 confirm.prepare」
	s := testServerForNotes(t)
	note := s.capabilityRequirementText(OpUIClick)
	if !strings.Contains(note, "界面写入") || !strings.Contains(note, "ui.write") {
		t.Fatalf("写类工具的能力要求文案应含「界面写入 / ui.write」：%s", note)
	}
	if !strings.Contains(note, "不需要 confirm.prepare") {
		t.Fatalf("写类工具应说明不需要 token：%s", note)
	}
	readNote := s.capabilityRequirementText(OpUILayout)
	if !strings.Contains(readNote, "永远允许") {
		t.Fatalf("读类工具的能力要求文案应说明永远允许：%s", readNote)
	}
	if ActionNeedsConfirm(string(CapUIWrite)) {
		t.Fatalf("ui.write 能力本身不应需要确认 token")
	}
}

// tools/list：25 个 ui.* 工具都要出现，且每个都带能力要求行与合法 schema。
func TestUIToolsListedInMCP(t *testing.T) {
	ensureAllRegistrations(t)
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	resp := ts.srv.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list",
	})
	raw, _ := json.Marshal(resp.Result)
	var out struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 tools/list 失败：%v", err)
	}
	// 工具总数不再写死（M7 应用控制面等后续各域会继续追加）：
	// 改为**注册表驱动** —— 断言每个登记过能力位的工具都出现在 tools/list。
	assertRegistryToolsListed(t, toolNamesOf(out.Tools))
	seen := map[string]bool{}
	for _, tool := range out.Tools {
		seen[tool.Name] = true
		if !strings.Contains(tool.Description, "能力要求：") {
			t.Fatalf("工具 %s 的描述缺少「能力要求」行", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("工具 %s 的入参 schema 不是 object：%v", tool.Name, tool.InputSchema)
		}
	}
	for _, name := range uiToolNames {
		if !seen[name] {
			t.Fatalf("tools/list 里缺少 %s", name)
		}
	}

	// 写类工具的描述必须声明「只作用于当前运行实例，刷新即还原，不写入设置」
	for _, tool := range out.Tools {
		if !uiWriteTools[tool.Name] || tool.Name == OpUIReload || tool.Name == OpUIWindow {
			continue
		}
		if !strings.Contains(tool.Description, "刷新") || !strings.Contains(tool.Description, "不写入设置") {
			t.Fatalf("写类工具 %s 的描述应声明内存态 / 不写入设置：%s", tool.Name, tool.Description)
		}
	}
	// ui.reload 必须警告会重挂前端
	for _, tool := range out.Tools {
		if tool.Name != OpUIReload {
			continue
		}
		if !strings.Contains(tool.Description, "重挂整个前端") {
			t.Fatalf("ui.reload 的描述必须警告会重挂整个前端：%s", tool.Description)
		}
	}
	// ui.window 必须提示最小化会让截图失败
	for _, tool := range out.Tools {
		if tool.Name != OpUIWindow {
			continue
		}
		if !strings.Contains(tool.Description, "最小化") || !strings.Contains(tool.Description, "会失败") {
			t.Fatalf("ui.window 的描述必须提示最小化会让截图失败：%s", tool.Description)
		}
	}
}

// ---- 6. ui.assert 判定逻辑 ----

func baseFacts() map[string]any {
	return map[string]any{
		"selector": ".topbar h1",
		"found":    true,
		"count":    1.0,
		"visible":  true,
		"rect":     map[string]any{"x": 10.0, "y": 10.0, "w": 100.0, "h": 20.0},
		"text":     "工作区",
		"styles":   map[string]any{"display": "flex", "color": "rgb(242, 245, 248)"},
	}
}

func TestUIAssertChecks(t *testing.T) {
	cases := []struct {
		name       string
		args       map[string]any
		wantPassed bool
		wantFailed []string
	}{
		{name: "全部通过", args: map[string]any{
			"selector": ".topbar h1", "exists": true, "visible": true, "text": "工作区",
			"textContains": "工作", "rectWithin": map[string]any{"x": 0.0, "y": 0.0, "w": 200.0, "h": 100.0},
			"styles": map[string]any{"display": "flex"},
		}, wantPassed: true},
		{name: "文本不符", args: map[string]any{"text": "服务器"}, wantPassed: false, wantFailed: []string{"text"}},
		{name: "可见性不符", args: map[string]any{"visible": false}, wantPassed: false, wantFailed: []string{"visible"}},
		{name: "包含失败", args: map[string]any{"textContains": "隧道"}, wantPassed: false, wantFailed: []string{"textContains"}},
		{name: "矩形越界", args: map[string]any{
			"rectWithin": map[string]any{"x": 0.0, "y": 0.0, "w": 50.0, "h": 50.0},
		}, wantPassed: false, wantFailed: []string{"rectWithin"}},
		{name: "样式不符", args: map[string]any{
			"styles": map[string]any{"display": "block", "color": "rgb(242, 245, 248)"},
		}, wantPassed: false, wantFailed: []string{"styles.display"}},
		{name: "样式大小写与空白不敏感", args: map[string]any{
			"styles": map[string]any{"display": " FLEX "},
		}, wantPassed: true},
		{name: "存在性相反", args: map[string]any{"exists": false}, wantPassed: false, wantFailed: []string{"exists"}},
		{name: "没有条件", args: map[string]any{}, wantPassed: false, wantFailed: []string{"no-checks"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"selector": ".topbar h1"}
			for k, v := range tc.args {
				args[k] = v
			}
			passed, checks := uiAssertChecks(baseFacts(), args)
			if passed != tc.wantPassed {
				t.Fatalf("passed = %v，期望 %v（checks=%v）", passed, tc.wantPassed, checks)
			}
			failed := []string{}
			for _, c := range checks {
				if !uiBool(c, "passed") {
					failed = append(failed, uiArgString(c, "name"))
					if uiArgString(c, "reason") == "" {
						t.Fatalf("失败项 %s 必须给出中文原因", c["name"])
					}
				}
			}
			if len(failed) != len(tc.wantFailed) {
				t.Fatalf("失败项 = %v，期望 %v", failed, tc.wantFailed)
			}
			for i := range failed {
				if failed[i] != tc.wantFailed[i] {
					t.Fatalf("失败项 = %v，期望 %v", failed, tc.wantFailed)
				}
			}
		})
	}

	// 元素不存在：exists=false 应通过，visible=true 应失败且原因说明「不存在」
	missing := map[string]any{"selector": ".x", "found": false, "count": 0.0, "visible": false, "rect": nil, "text": "", "styles": map[string]any{}}
	passed, checks := uiAssertChecks(missing, map[string]any{"selector": ".x", "exists": false})
	if !passed {
		t.Fatalf("元素不存在时 exists=false 应通过：%v", checks)
	}
	passed, checks = uiAssertChecks(missing, map[string]any{"selector": ".x", "visible": true})
	if passed {
		t.Fatalf("元素不存在时 visible=true 应失败")
	}
	if !strings.Contains(uiArgString(checks[0], "reason"), "不存在") {
		t.Fatalf("失败原因应说明元素不存在：%v", checks[0])
	}
}

// ui.assert 走完整分发路径时，必须把 selector / props 传给前端并返回逐条结果。
func TestUIAssertDispatch(t *testing.T) {
	fe := &stubUI{replies: map[string]any{uiOpElementFacts: baseFacts()}}
	s := New(Options{Handler: &fakeHandler{}})
	res, err := s.uiHandle(context.Background(), fe, OpUIAssert, map[string]any{
		"selector": ".topbar h1", "textContains": "工作",
		"styles": map[string]any{"display": "flex"},
	})
	if err != nil {
		t.Fatalf("ui.assert 失败：%v", err)
	}
	if !uiBool(res.Data, "passed") {
		t.Fatalf("应通过：%v", res.Data)
	}
	if !fe.called(uiOpElementFacts) {
		t.Fatalf("应调用前端元素事实 op：%v", fe.calls)
	}
	props, _ := fe.args[0]["props"].([]string)
	if len(props) != 1 || props[0] != "display" {
		t.Fatalf("应把要断言的样式属性传给前端：%v", fe.args[0]["props"])
	}
	if _, err := s.uiHandle(context.Background(), fe, OpUIAssert, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "selector") {
		t.Fatalf("缺少 selector 应返回中文错误：%v", err)
	}
}

// ---- 7. 截断与体积上限 ----

func TestUIClipAndSizeLimit(t *testing.T) {
	if got := uiClipString("abcdef", 3); !strings.Contains(got, "abc") || !strings.Contains(got, "已截断 3 字符") {
		t.Fatalf("字符串截断应带标记：%s", got)
	}
	if got := uiClipString("abc", 10); got != "abc" {
		t.Fatalf("未超长不应改动：%s", got)
	}

	// 数组截断：末项是截断标记
	arr := make([]any, 50)
	for i := range arr {
		arr[i] = float64(i)
	}
	clipped, ok := uiClipValue(arr, 10, 5, 10, 0).([]any)
	if !ok || len(clipped) != 6 {
		t.Fatalf("数组应裁到 5 项 + 1 条标记，实际 %v", clipped)
	}
	last, _ := clipped[5].(map[string]any)
	if !uiBool(last, "truncated") || !strings.Contains(uiArgString(last, "truncatedNote"), "还有 45 项") {
		t.Fatalf("数组截断标记不对：%v", last)
	}

	// 对象裁剪：按键名排序保留前 N 个（顺序稳定）
	big := map[string]any{}
	for _, k := range []string{"z", "a", "m", "b"} {
		big[k] = k
	}
	obj, _ := uiClipValue(big, 10, 10, 2, 0).(map[string]any)
	if !uiBool(obj, "truncated") || obj["a"] != "a" || obj["b"] != "b" {
		t.Fatalf("对象裁剪应按键名排序保留前 2 个：%v", obj)
	}

	// 体积上限：超限时保留标量、复杂字段替换为形状描述
	huge := "x" + strings.Repeat("漢", 400000)
	data := map[string]any{
		"ok":    true,
		"count": 3,
		"list":  []any{huge, huge},
		"big":   huge,
	}
	limited := uiLimitData(data, 1024, OpUIQuery)
	if !uiBool(limited, "truncated") {
		t.Fatalf("超大结果应被标注截断：%v", limited["truncatedNote"])
	}
	if limited["ok"] != true || limited["count"] != 3 {
		t.Fatalf("标量字段应保留：%v", limited)
	}
	if s, isStr := limited["list"].(string); !isStr || !strings.Contains(s, "已省略数组") {
		t.Fatalf("复杂字段应替换成形状描述，实际 %#v", limited["list"])
	}
	// 单个超长标量也必须被截断（否则「保留标量」就等于没限额）
	if s, isStr := limited["big"].(string); !isStr || !strings.Contains(s, "已截断") || len(s) > 4096 {
		t.Fatalf("超长标量字段必须被截断，实际长度 %d", len(uiArgString(limited, "big")))
	}
	if !strings.Contains(uiArgString(limited, "truncatedNote"), "字节上限") {
		t.Fatalf("截断说明应提到字节上限：%v", limited)
	}
	b, _ := json.Marshal(limited)
	if len(b) > 4096 {
		t.Fatalf("兜底后仍过大：%d 字节", len(b))
	}
	// 未超限时原样返回
	small := map[string]any{"a": 1}
	if got := uiLimitData(small, 1024, OpUIQuery); len(got) != 1 {
		t.Fatalf("未超限应原样返回：%v", got)
	}
}

// ui.console 的级别 / 时间 / 条数过滤（在 Go 侧做，便于测试与统一语义）。
func TestUIConsoleFilter(t *testing.T) {
	entries := []map[string]any{
		{"ts": 100.0, "level": "debug", "text": []any{"d"}},
		{"ts": 200.0, "level": "info", "text": []any{"i"}},
		{"ts": 300.0, "level": "warn", "text": []any{"w"}},
		{"ts": 400.0, "level": "error", "text": []any{"e"}},
		{"ts": 500.0, "level": "weird", "text": []any{"?"}},
	}
	if got := uiConsoleFilter(entries, "", 0, 10); len(got) != 5 {
		t.Fatalf("无过滤应返回全部，实际 %d", len(got))
	}
	if got := uiConsoleFilter(entries, "warn", 0, 10); len(got) != 2 {
		t.Fatalf("level=warn 应只返回 warn 及以上，实际 %d", len(got))
	}
	if got := uiConsoleFilter(entries, "", 350, 10); len(got) != 2 {
		t.Fatalf("since 过滤后应剩 2 条，实际 %d", len(got))
	}
	if got := uiConsoleFilter(entries, "", 0, 2); len(got) != 2 || uiNum(got[0]["ts"]) != 400 {
		t.Fatalf("limit 应保留最新的 N 条：%v", got)
	}
	if rank := uiLevelRank("ERROR"); rank != 3 {
		t.Fatalf("级别大小写不敏感，实际 rank=%d", rank)
	}

	fe := &stubUI{replies: map[string]any{OpUIConsole: map[string]any{
		"capacity": 200.0,
		"entries":  toAnySlice(entries),
	}}}
	s := New(Options{Handler: &fakeHandler{}})
	res, err := s.uiHandle(context.Background(), fe, OpUIConsole, map[string]any{"level": "error", "limit": 5})
	if err != nil {
		t.Fatalf("ui.console 失败：%v", err)
	}
	if uiNum(res.Data["count"]) != 1 || uiNum(res.Data["buffered"]) != 5 {
		t.Fatalf("过滤结果不对：%v", res.Data)
	}
}

func toAnySlice(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
}

// 读类工具的返回必须体积有界：构造 200 条超长文本，验证裁剪与上限标注。
func TestUIQueryAndTreeBounded(t *testing.T) {
	long := strings.Repeat("很长的文本", 400)
	matches := make([]any, 0, 200)
	for i := 0; i < 200; i++ {
		matches = append(matches, map[string]any{
			"index": float64(i), "tag": "div", "id": "x", "classes": []any{"a", "b"},
			"text": long, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 10.0, "h": 10.0},
			"visible": true, "attrs": map[string]any{}, "childCount": 1.0,
		})
	}
	fe := &stubUI{replies: map[string]any{
		OpUIQuery: map[string]any{"total": 200.0, "matches": matches},
		OpUITree:  map[string]any{"nodes": matches, "total": 200.0, "truncated": true},
	}}
	s := New(Options{Handler: &fakeHandler{}})

	res, err := s.uiHandle(context.Background(), fe, OpUIQuery, map[string]any{"selector": "div", "limit": 5})
	if err != nil {
		t.Fatalf("ui.query 失败：%v", err)
	}
	if uiNum(res.Data["count"]) != 5 || uiNum(res.Data["total"]) != 200 {
		t.Fatalf("limit 未生效：%v", res.Data["count"])
	}
	if !uiBool(res.Data, "truncated") || !strings.Contains(uiArgString(res.Data, "truncatedNote"), "只返回前 5 个") {
		t.Fatalf("应给出明确的截断说明：%v", res.Data)
	}
	b, _ := json.Marshal(res.Data)
	if len(b) > uiMaxDataBytes {
		t.Fatalf("ui.query 返回超过 %d 字节上限：%d", uiMaxDataBytes, len(b))
	}
	// 单条文本必须被截断（默认 200 字）
	items, _ := res.Data["matches"].([]any)
	first, _ := items[0].(map[string]any)
	if !strings.Contains(uiArgString(first, "text"), "已截断") {
		t.Fatalf("元素文本应被截断：%s", first["text"])
	}

	// limit 上限收敛
	res, err = s.uiHandle(context.Background(), fe, OpUIQuery, map[string]any{"selector": "div", "limit": 99999})
	if err != nil {
		t.Fatalf("ui.query 失败：%v", err)
	}
	if uiNum(res.Data["count"]) != uiQueryMaxLimit {
		t.Fatalf("limit 应收敛到 %d，实际 %v", uiQueryMaxLimit, res.Data["count"])
	}
	// 缺 selector → 中文错误
	if _, err := s.uiHandle(context.Background(), fe, OpUIQuery, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "selector") {
		t.Fatalf("缺 selector 应返回中文错误：%v", err)
	}

	res, err = s.uiHandle(context.Background(), fe, OpUITree, map[string]any{"depth": 99, "limit": 9999})
	if err != nil {
		t.Fatalf("ui.tree 失败：%v", err)
	}
	if uiNum(res.Data["depth"]) != uiTreeMaxDepth || uiNum(res.Data["limit"]) != uiTreeMaxLimit {
		t.Fatalf("depth/limit 应收敛：%v / %v", res.Data["depth"], res.Data["limit"])
	}
	if !uiBool(res.Data, "truncated") {
		t.Fatalf("组件树被截断时应标注：%v", res.Data)
	}
	if !uiBool(res.Data, "truncated") || !strings.Contains(uiArgString(res.Data, "truncatedNote"), "depth=") {
		t.Fatalf("组件树截断说明不对：%v", res.Data)
	}
}

// ---- 8. 布局重叠 ----

func TestUILayoutOverlaps(t *testing.T) {
	entries := []map[string]any{
		// topbar 包含 terminal（祖先-后代，不算重叠）
		{"name": "topbar", "visible": true, "inViewport": true, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 100.0, "h": 20.0}, "contains": []any{"terminal"}},
		{"name": "terminal", "visible": true, "inViewport": true, "rect": map[string]any{"x": 0.0, "y": 20.0, "w": 100.0, "h": 100.0}},
		// 命令面板是浮层：盖住 topbar 属预期
		{"name": "commandPalette", "visible": true, "inViewport": true, "overlay": true, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 100.0, "h": 20.0}},
		// 两个普通元素发生部分重叠 → 非预期
		{"name": "nav", "visible": true, "inViewport": true, "rect": map[string]any{"x": 0.0, "y": 200.0, "w": 50.0, "h": 50.0}},
		{"name": "sidebar", "visible": true, "inViewport": true, "rect": map[string]any{"x": 40.0, "y": 240.0, "w": 50.0, "h": 50.0}},
		// 另一对非预期重叠
		{"name": "toast", "visible": true, "inViewport": true, "rect": map[string]any{"x": 0.0, "y": 300.0, "w": 100.0, "h": 20.0}},
		{"name": "footer", "visible": true, "inViewport": true, "rect": map[string]any{"x": 50.0, "y": 310.0, "w": 100.0, "h": 20.0}},
		// 不可见 / 已滚出视口：都不参与重叠判定
		{"name": "hidden", "visible": false, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 100.0, "h": 100.0}},
		{"name": "scrolledOut", "visible": true, "inViewport": false, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 100.0, "h": 100.0}},
	}
	raw := uiLayoutOverlaps(entries)
	if len(raw) != 3 {
		t.Fatalf("应得到 3 对重叠（祖先-后代 / 不可见 / 视口外的组合都应排除），实际 %d：%v", len(raw), raw)
	}
	seen := map[string]bool{}
	for _, item := range raw {
		m, _ := item.(map[string]any)
		pair := uiArgString(m, "a") + "|" + uiArgString(m, "b")
		seen[pair] = true
		if !strings.Contains(uiArgString(m, "reason"), "重叠") {
			t.Fatalf("重叠项应给出中文原因：%v", m)
		}
		expected := uiBool(m, "expected")
		switch pair {
		case "topbar|commandPalette":
			if !expected {
				t.Fatalf("浮层参与的重叠应标为预期（expected=true）：%v", m)
			}
		case "nav|sidebar", "toast|footer":
			if expected {
				t.Fatalf("普通元素之间的重叠不应标为预期：%v", m)
			}
		default:
			t.Fatalf("出现意外的重叠对 %s（祖先关系 / 不可见元素 / 视口外元素应被排除）：%v", pair, m)
		}
	}
	for _, want := range []string{"topbar|commandPalette", "nav|sidebar", "toast|footer"} {
		if !seen[want] {
			t.Fatalf("缺少重叠对 %s：%v", want, seen)
		}
	}
	// 面积最大的排在最前（便于 AI 先看最严重的）
	first, _ := raw[0].(map[string]any)
	if uiNum(first["area"]) < 500 {
		t.Fatalf("应按交集面积从大到小排序，第一项面积 = %v", first["area"])
	}
}

// ---- 9. 写类工具的参数校验 ----

func TestUIWriteValidation(t *testing.T) {
	cases := []struct {
		op      string
		args    map[string]any
		wantErr string
	}{
		{OpUISetToken, map[string]any{"tokens": map[string]any{"radius": "8px"}}, "必须以 -- 开头"},
		{OpUISetToken, map[string]any{"reset": []any{"radius"}}, "必须以 -- 开头"},
		{OpUISetToken, map[string]any{}, "需要 tokens"},
		{OpUIInjectCSS, map[string]any{"id": "x"}, "缺少 css"},
		{OpUIInjectCSS, map[string]any{"css": ""}, "必须给 id"},
		{OpUIStorePatch, map[string]any{"patch": map[string]any{"a": 1}}, "缺少 name"},
		{OpUIStorePatch, map[string]any{"name": "ui"}, "缺少 patch"},
		{OpUINavigate, map[string]any{"view": "nope"}, "workspace | servers | tunnel | settings"},
		{OpUIType, map[string]any{"selector": "#x"}, "缺少 text"},
		{OpUIKey, map[string]any{}, "缺少 key"},
	}
	s := New(Options{Handler: &fakeHandler{}})
	for _, tc := range cases {
		fe := &stubUI{}
		_, err := s.uiHandle(context.Background(), fe, tc.op, tc.args)
		if err == nil {
			t.Fatalf("%s 的参数 %v 应被拒绝", tc.op, tc.args)
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s 的错误文案应含 %q，实际：%v", tc.op, tc.wantErr, err)
		}
		if len(fe.calls) != 0 {
			t.Fatalf("参数非法时不应调用前端（%s）", tc.op)
		}
	}
	// 合法参数应透传，并带上「不落库」的提示
	fe := &stubUI{replies: map[string]any{OpUIStorePatch: map[string]any{"changedKeys": []any{"view"}}}}
	res, err := s.uiHandle(context.Background(), fe, OpUIStorePatch, map[string]any{"name": "ui", "patch": map[string]any{"view": "settings"}})
	if err != nil {
		t.Fatalf("合法 storePatch 应通过：%v", err)
	}
	if !strings.Contains(uiArgString(res.Data, "persistenceHint"), "不会调用任何 save()") {
		t.Fatalf("storePatch 应给出落库提示：%v", res.Data)
	}
}

// ui.hit 缺少坐标、ui.styles 缺少选择器都要给中文错误。
func TestUIReadArgsValidation(t *testing.T) {
	fe := &stubUI{}
	s := New(Options{Handler: &fakeHandler{}})
	for _, tc := range []struct {
		op, want string
		args     map[string]any
	}{
		{OpUIHit, "缺少 x / y", map[string]any{}},
		{OpUIHit, "缺少 y", map[string]any{"x": 1}},
		{OpUIStyles, "缺少 selector", map[string]any{}},
	} {
		if _, err := s.uiHandle(context.Background(), fe, tc.op, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s 应返回「%s」错误，实际：%v", tc.op, tc.want, err)
		}
	}
	if _, err := s.uiHandle(context.Background(), fe, "ui.nope", nil); err == nil || !strings.Contains(err.Error(), "未知的 UI 操作") {
		t.Fatalf("未知 op 应返回中文错误：%v", err)
	}
}

// ---- 10. HTTP 路由：能力校验与审计工具名 ----

func TestUIHTTPRoutes(t *testing.T) {
	// ui.write 未开启：写类路由 403（中文原因），读类路由 200
	deniedBase, audit := startHTTP(t, &stubGate{denied: map[Capability]string{
		CapUIWrite: "「界面写入」能力未开启：请在 设置 → 调试模式 → MCP 能力 中开启",
	}}, &stubExecutor{})

	status, body := httpDo(t, deniedBase, http.MethodGet, "/v1/ui/revision", nil)
	if status != http.StatusOK {
		t.Fatalf("读类路由（ui.revision）应放行，实际 %d：%s", status, body)
	}
	status, body = httpDo(t, deniedBase, http.MethodGet, "/v1/ui/layout", nil)
	if status != http.StatusOK {
		t.Fatalf("读类路由（ui.layout）应放行，实际 %d：%s", status, body)
	}
	status, body = httpDo(t, deniedBase, http.MethodPost, "/v1/ui/click", map[string]any{"selector": "#x"})
	if status != http.StatusForbidden {
		t.Fatalf("ui.write 未开启时写类路由应 403，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "界面写入") || !strings.Contains(body, "MCP 能力") {
		t.Fatalf("403 应返回中文原因与开启路径：%s", body)
	}
	// 审计里应记成与 MCP 同名的工具
	recs := audit.Recent(10, "", "http")
	foundTool := map[string]bool{}
	for _, r := range recs {
		foundTool[r.Tool] = true
	}
	if !foundTool[OpUIRevision] || !foundTool[OpUILayout] || !foundTool[OpUIClick] {
		t.Fatalf("HTTP 路由的审计工具名应与 MCP 工具同名：%v", foundTool)
	}
	// 写类路由不需要 confirmToken（可逆 / 高频）
	for _, r := range recs {
		if r.Tool == OpUIClick && strings.Contains(r.Error, "confirm.prepare") {
			t.Fatalf("ui.* 写类路由不应要求 confirm token：%+v", r)
		}
	}

	// ui.write 开启：写类路由通过
	openBase, _ := startHTTP(t, &stubGate{open: map[Capability]bool{CapUIWrite: true}}, &stubExecutor{})
	status, body = httpDo(t, openBase, http.MethodPost, "/v1/ui/click", map[string]any{"selector": "#x"})
	if status != http.StatusOK {
		t.Fatalf("ui.write 开启后写类路由应成功，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "ui.click") {
		t.Fatalf("响应应包含本次 op：%s", body)
	}
	status, body = httpDo(t, openBase, http.MethodPost, "/v1/ui/set-token", map[string]any{"tokens": map[string]any{"--radius": "16px"}})
	if status != http.StatusOK {
		t.Fatalf("ui.setToken 应成功，实际 %d：%s", status, body)
	}
}

// ---- 11. 资源与通知映射 ----

func TestUIResourcesAndNotifications(t *testing.T) {
	fe := &stubUI{replies: map[string]any{
		OpUILayout:  map[string]any{"entries": []any{}, "viewport": map[string]any{"width": 100.0}},
		OpUIConsole: map[string]any{"entries": []any{map[string]any{"level": "warn", "text": []any{"注意"}}}},
		uiOpView:    map[string]any{"view": "settings", "tabCount": 2.0},
	}}
	s := New(Options{Handler: &fakeHandler{}})
	ctx := context.Background()
	for _, uri := range uiResourceURIs {
		// 直接打 ui.go 里的资源读取（与 /mcp resources/read 的分发一致）
		s.opts.Handler = fakeHandlerFor(fe)
		out, err := s.mcpReadUIResource(ctx, uri)
		if err != nil {
			t.Fatalf("读取资源 %s 失败：%v", uri, err)
		}
		if out == "" {
			t.Fatalf("资源 %s 返回为空", uri)
		}
	}
	if !isUIResource("ui://layout") || isUIResource("app://state") {
		t.Fatalf("isUIResource 判定不对")
	}
	if mime := mcpResourceMimeType("ui://console"); mime != "text/plain" {
		t.Fatalf("ui://console 应是 text/plain，实际 %s", mime)
	}
	if mime := mcpResourceMimeType("ui://layout"); mime != "application/json" {
		t.Fatalf("ui://layout 应是 application/json，实际 %s", mime)
	}

	// Hub 事件 → notifications/resources/updated
	ev := Event{Topic: "ui.view", Name: "ui:view", Data: json.RawMessage(`{"view":"settings"}`)}
	note, ok := uiResourceUpdatedNotification(ev)
	if !ok {
		t.Fatalf("ui.view 事件应映射成 resources/updated")
	}
	if note["method"] != "notifications/resources/updated" {
		t.Fatalf("通知方法不对：%v", note["method"])
	}
	params, _ := note["params"].(map[string]any)
	if params["uri"] != "ui://view" {
		t.Fatalf("通知 uri 不对：%v", params)
	}
	if _, ok := uiResourceUpdatedNotification(Event{Topic: "ssh.output"}); ok {
		t.Fatalf("非 ui.* 事件不应被映射")
	}
}

// fakeHandlerFor 让「Handler → 前端」这条链路在测试里指向替身前端
// （生产里 debugController 把 ui.* op 透传给 Wails 前端桥）。
func fakeHandlerFor(fe uiFrontend) Handler { return &uiForwardHandler{fe: fe} }

type uiForwardHandler struct{ fe uiFrontend }

func (h *uiForwardHandler) Handle(ctx context.Context, op string, args json.RawMessage) (any, error) {
	var m map[string]any
	if len(args) > 0 {
		_ = json.Unmarshal(args, &m)
	}
	return h.fe.CallUI(ctx, op, m)
}

// ---- 12. 观察器：前端不支持 ui.revision 时自行退出（不留后台轮询）----

func TestUIWatcherStopsWhenFrontendUnsupported(t *testing.T) {
	hub := NewHub()
	s := New(Options{Handler: &fakeHandler{}, Hub: hub})
	w := startUIWatcher(s)
	if w == nil {
		t.Fatalf("应能启动观察器")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		uiWatchersMu.Lock()
		_, alive := uiWatchers[s]
		uiWatchersMu.Unlock()
		if !alive {
			return // 已自行退出：符合预期（替身前端没有 revision 字段）
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("前端不支持 ui.revision 时观察器应自行退出，避免无意义轮询")
}

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// ---- 13. 活体自测替代：整条 MCP 链路上的原始响应（默认跳过，需要 DSH_UI_PROBE=1）----
//
// 应用未运行时，用「真实 HTTP + 真实中间件 + 真实 MCP 分发 + 替身前端」把 ui.* 工具
// 逐个跑一遍并把原始响应打到 stdout（各截断 400 字），便于人工复核返回结构。
// 用法：$env:DSH_UI_PROBE=1; go test -count=1 -run TestManualUIProbe -v ./internal/debugsrv/
func TestManualUIProbe(t *testing.T) {
	if os.Getenv("DSH_UI_PROBE") == "" {
		t.Skip("设置 DSH_UI_PROBE=1 才执行 UI 活体探测（会产生 stdout 输出）")
	}
	logx.SetLogDirForTest(t.TempDir())
	t.Cleanup(func() { logx.SetLogDirForTest("") })

	s := New(Options{
		Addr: "127.0.0.1:0", Token: "test-token", Handler: &uiProbeHandler{},
		Hub: NewHub(), Gate: &stubGate{open: map[Capability]bool{CapUIWrite: true}},
		Audits: NewAuditLog(), Confirms: NewConfirmManager(DefaultConfirmTTL), Executor: &stubExecutor{},
		Logf: func(string, ...any) {},
	})
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("启动失败：%v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	})

	// 预置两张快照，让 ui.diff 能真正跑通（像素差异 + 指纹差异 + diff 图）
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.RGBA{R: 255, A: 255}
	basePNG, _ := uiEncodePNG(solidImage(64, 64, white))
	afterImg := solidImage(64, 64, white)
	for y := 0; y < 16; y++ {
		for x := 32; x < 48; x++ {
			afterImg.SetRGBA(x, y, red)
		}
	}
	afterPNG, _ := uiEncodePNG(afterImg)
	writeFakeSnapshot(t, "probe-base", basePNG, 1.0, []map[string]any{
		{"selectorPath": "#app > div:nth-of-type(1)", "tag": "div", "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 64.0, "h": 64.0}, "classes": []any{"shell"}, "text": "", "zIndex": "auto", "visible": true},
	})
	writeFakeSnapshot(t, "probe-after", afterPNG, 1.0, []map[string]any{
		{"selectorPath": "#app > div:nth-of-type(1)", "tag": "div", "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 64.0, "h": 64.0}, "classes": []any{"shell", "wide"}, "text": "改了", "zIndex": "auto", "visible": true},
	})

	base := "http://" + addr
	call := func(name string, args map[string]any) string {
		status, body := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": args},
		})
		if status != http.StatusOK {
			return fmt.Sprintf("[HTTP %d] %s", status, body)
		}
		var resp struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
					Data string `json:"data"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return "解析失败: " + body
		}
		out := ""
		for _, c := range resp.Result.Content {
			if c.Type == "image" {
				out += fmt.Sprintf("[image/png base64 %d 字节] ", len(c.Data))
				continue
			}
			out += truncateForLog(strings.ReplaceAll(c.Text, "\n", " "), 400)
		}
		if resp.Result.IsError {
			out = "[isError] " + out
		}
		return out
	}

	// 先确认工具总数与资源清单
	status, listBody := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	var listResp struct {
		Result struct {
			Tools []mcpTool `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(listBody), &listResp)
	var uiNames []string
	for _, tool := range listResp.Result.Tools {
		if strings.HasPrefix(tool.Name, "ui.") {
			uiNames = append(uiNames, tool.Name)
		}
	}
	status, resBody := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "resources/list",
	})

	fmt.Println("=== UI PROBE ===")
	fmt.Printf("tools/list [HTTP %d]：共 %d 个工具，其中 ui.* %d 个：%s\n",
		status, len(listResp.Result.Tools), len(uiNames), strings.Join(uiNames, " "))
	fmt.Printf("resources/list：%s\n", truncateForLog(strings.ReplaceAll(resBody, "\n", " "), 400))

	steps := []struct {
		name string
		args map[string]any
	}{
		{"ui.layout", nil},
		{"ui.query", map[string]any{"selector": ".xterm", "limit": 2}},
		{"ui.styles", map[string]any{"selector": ".topbar"}},
		{"ui.tokens", map[string]any{"filter": "signal"}},
		{"ui.hit", map[string]any{"x": 400, "y": 200}},
		{"ui.tree", map[string]any{"depth": 2, "limit": 3}},
		{"ui.console", map[string]any{"limit": 5}},
		{"ui.revision", nil},
		{"ui.assert", map[string]any{"selector": ".topbar h1", "visible": true, "styles": map[string]any{"display": "flex"}}},
		{"ui.store", map[string]any{"name": "ui"}},
		{"ui.store", map[string]any{}},
		{"ui.setToken", map[string]any{"tokens": map[string]any{"--radius": "16px"}}},
		{"ui.injectCSS", map[string]any{"css": ".topbar{outline:1px solid red}", "id": "probe"}},
		{"ui.storePatch", map[string]any{"name": "ui", "patch": map[string]any{"view": "settings"}}},
		{"ui.navigate", map[string]any{"view": "settings", "section": "debug"}},
		{"ui.click", map[string]any{"selector": ".nav-item"}},
		{"ui.type", map[string]any{"selector": "#masterPwd", "text": "hunter2", "clear": true}},
		{"ui.key", map[string]any{"key": "Escape"}},
		{"ui.hover", map[string]any{"selector": ".nav-item"}},
		{"ui.scroll", map[string]any{"selector": ".xterm-viewport", "dy": 120}},
		{"ui.window", map[string]any{"width": 1280, "height": 800}},
		{"ui.reload", nil},
		{"ui.diff", map[string]any{"a": "probe-base", "b": "probe-after"}},
		{"ui.snapshot", map[string]any{"name": "probe-live"}},
		{"ui.screenshot", map[string]any{"selector": ".xterm"}},
		{"ui.elementShot", map[string]any{"selector": ".topbar"}},
	}
	for _, st := range steps {
		fmt.Printf("%s → %s\n", st.name, call(st.name, st.args))
	}
	fmt.Println("=== END UI PROBE ===")
}

// uiProbeHandler 模拟前端桥：为每个 ui.* op 返回结构真实的假数据
// （生产里等价于 frontend/src/debug/ui.ts 的返回值）。
type uiProbeHandler struct{}

func (h *uiProbeHandler) Handle(_ context.Context, op string, args json.RawMessage) (any, error) {
	var in map[string]any
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	rect := map[string]any{"x": 0.0, "y": 20.0, "w": 900.0, "h": 600.0}
	viewport := map[string]any{"width": 1280.0, "height": 769.0, "dpr": 1.25, "uiScale": 1.0}
	window := map[string]any{
		"viewport": viewport,
		"outer":    map[string]any{"w": 1280.0, "h": 800.0},
		"inner":    map[string]any{"w": 1280.0, "h": 769.0},
	}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range window {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	switch op {
	case OpUILayout:
		return with(map[string]any{
			"entries": []any{
				map[string]any{"name": "topbar", "selector": ".topbar", "found": true, "visible": true, "inViewport": true,
					"rect": map[string]any{"x": 0.0, "y": 0.0, "w": 1280.0, "h": 48.0}, "zIndex": "auto", "overflow": "visible/visible",
					"pointerEvents": "auto", "contains": []any{}, "classes": []any{"topbar"}, "tag": "header"},
				map[string]any{"name": "nav", "selector": "aside.nav", "found": true, "visible": true, "inViewport": true,
					"rect": map[string]any{"x": 0.0, "y": 48.0, "w": 200.0, "h": 721.0}, "zIndex": "auto", "overflow": "auto/auto",
					"pointerEvents": "auto", "contains": []any{}, "classes": []any{"nav"}, "tag": "aside"},
				map[string]any{"name": "terminal", "selector": ".terminal-bg", "found": true, "visible": true, "inViewport": true,
					"rect": rect, "zIndex": "auto", "overflow": "hidden/hidden", "pointerEvents": "auto",
					"contains": []any{}, "classes": []any{"terminal-bg"}, "tag": "div"},
				map[string]any{"name": "commandPalette", "selector": ".modal[aria-label=\"命令面板\"]", "found": true, "visible": true, "inViewport": true,
					"overlay": true, "rect": map[string]any{"x": 360.0, "y": 200.0, "w": 560.0, "h": 360.0}, "zIndex": "50",
					"overflow": "hidden/hidden", "pointerEvents": "auto", "contains": []any{}, "classes": []any{"modal"}, "tag": "div"},
			},
			"view": map[string]any{"view": "workspace", "cmdOpen": true, "tabCount": 1.0, "revision": 3.0},
		}), nil
	case OpUIQuery:
		return map[string]any{"total": 4.0, "matches": []any{
			map[string]any{"index": 0.0, "tag": "div", "id": "", "classes": []any{"xterm", "enable-mouse-events"}, "text": "user@host:~$ ls -la", "rect": rect, "visible": true, "inViewport": true, "childCount": 3.0, "attrs": map[string]any{"aria-hidden": "true"}},
			map[string]any{"index": 1.0, "tag": "div", "id": "", "classes": []any{"xterm"}, "text": "", "rect": rect, "visible": true, "inViewport": true, "childCount": 2.0, "attrs": map[string]any{}},
		}}, nil
	case OpUIStyles:
		return map[string]any{"items": []any{
			map[string]any{"index": 0.0, "tag": "header", "id": "", "classes": []any{"topbar"}, "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 1280.0, "h": 48.0}, "visible": true,
				"styles":   map[string]any{"display": "flex", "position": "sticky", "z-index": "20", "color": "rgb(242, 245, 248)", "background-color": "rgb(14, 17, 22)"},
				"cssVars":  map[string]any{"--signal-500": "#2aa89a", "--radius-lg": "14px", "--nav-w": "220px"},
				"indexPic": nil},
		}, "total": 1.0}, nil
	case OpUITokens:
		return map[string]any{
			"element": "html", "tone": "dark",
			"theme":  map[string]any{"mode": "preset", "presetId": "signal", "presetName": "信号青绿", "baseTone": "auto", "resolvedTone": "dark"},
			"inline": map[string]any{"--signal-500": "#2aa89a", "--font-sans": "\"Inter\", sans-serif"},
			"computed": map[string]any{"--signal-500": "#2aa89a", "--signal-400": "#3ec4b4",
				"--ink-950": "#0a0c10", "--mist-100": "#f2f5f8", "--radius-lg": "14px", "--nav-w-expanded": "220px"},
			"count": 6.0, "viewport": map[string]any{"dpr": 1.25, "uiScale": 1.0, "width": 1280.0, "height": 769.0},
		}, nil
	case OpUIHit:
		return map[string]any{"x": 400.0, "y": 200.0, "chain": []any{
			map[string]any{"index": 0.0, "tag": "canvas", "id": "", "classes": []any{"xterm-text-layer"}, "pointerEvents": "auto", "zIndex": "auto", "position": "absolute", "rect": rect, "text": ""},
			map[string]any{"index": 1.0, "tag": "div", "id": "", "classes": []any{"xterm-screen"}, "pointerEvents": "auto", "zIndex": "auto", "position": "relative", "rect": rect, "text": ""},
		}}, nil
	case OpUITree:
		return map[string]any{"nodes": []any{
			map[string]any{"name": "App", "key": nil, "depth": 0.0, "dom": map[string]any{"tag": "div", "id": "app", "classes": []any{}}, "propsSummary": map[string]any{}, "childCount": 9.0, "uid": 0.0},
			map[string]any{"name": "TerminalView", "key": nil, "depth": 1.0, "dom": map[string]any{"tag": "div", "id": "", "classes": []any{"relative"}}, "propsSummary": map[string]any{"tab": "[Object keys: clientId, serverName, status]"}, "childCount": 3.0, "uid": 12.0},
		}, "total": 2.0, "truncated": true}, nil
	case OpUIConsole:
		return map[string]any{"capacity": 200.0, "entries": []any{
			map[string]any{"ts": 1000.0, "level": "log", "text": []any{"[xterm] 初始化 WebGL 渲染器"}},
			map[string]any{"ts": 2000.0, "level": "warn", "text": []any{"WebGL 上下文丢失，降级 Canvas"}, "source": "xterm"},
			map[string]any{"ts": 3000.0, "level": "error", "text": []any{"TypeError: Cannot read properties of null"}, "stack": "at TerminalView.vue:1204"},
		}}, nil
	case OpUIRevision:
		return map[string]any{"revision": 3.0, "loadedAt": 1700000000000.0, "timeOrigin": 1699999999000.0, "hotReloaded": true,
			"href": "wails://wails/", "view": map[string]any{"view": "workspace", "tabCount": 1.0}, "viewport": viewport}, nil
	case uiOpView:
		return map[string]any{"view": "workspace", "cmdOpen": false, "sftpVisible": true, "rightPanel": "sftp", "splitShown": false, "activeId": "tab-1", "tabCount": 1.0, "revision": 3.0}, nil
	case uiOpGeometry:
		sel := ""
		if in != nil {
			sel, _ = in["selector"].(string)
		}
		return with(map[string]any{"selector": sel, "found": sel != "", "visible": sel != "", "rect": rect, "clip": nil,
			"tag": "div", "classes": []any{"terminal-bg"}, "zIndex": "auto"}), nil
	case uiOpFingerprint:
		return with(map[string]any{"fingerprint": []any{
			map[string]any{"selectorPath": "#app > div:nth-of-type(1)", "tag": "div", "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 1280.0, "h": 769.0}, "classes": []any{"app-shell"}, "text": "", "zIndex": "auto", "visible": true, "styles": map[string]any{"display": "block"}},
			map[string]any{"selectorPath": "#app > div:nth-of-type(1) > aside:nth-of-type(1)", "tag": "aside", "rect": map[string]any{"x": 0.0, "y": 0.0, "w": 220.0, "h": 769.0}, "classes": []any{"nav"}, "text": "工作区 服务器 隧道 设置", "zIndex": "auto", "visible": true, "styles": map[string]any{"display": "flex"}},
		}, "count": 2.0, "truncated": false, "revision": 3.0, "view": map[string]any{"view": "workspace"}}), nil
	case uiOpElementFacts:
		return map[string]any{"selector": ".topbar h1", "found": true, "count": 1.0, "visible": true,
			"rect": map[string]any{"x": 220.0, "y": 12.0, "w": 80.0, "h": 24.0}, "text": "工作区", "zIndex": "auto",
			"styles": map[string]any{"display": "block", "font-size": "17px"}}, nil
	case OpUIStore:
		name, _ := in["name"].(string)
		if name == "" {
			return map[string]any{"stores": []any{"credentials", "groups", "servers", "sessions", "settings", "ui"}, "count": 6.0}, nil
		}
		return map[string]any{"name": name, "path": nil, "keys": []any{"view", "cmdOpen", "terminalSidebarOpen"},
			"state": map[string]any{"view": "workspace", "cmdOpen": false, "terminalSidebarOpen": true},
			"bytes": 78.0, "truncated": false, "persisted": false}, nil
	case OpUISetToken:
		return map[string]any{"element": "html", "applied": map[string]any{"--radius": "16px"}, "removed": []any{}, "appliedCount": 1.0, "removedCount": 0.0, "persisted": false}, nil
	case OpUIInjectCSS:
		return map[string]any{"injected": true, "id": "probe", "bytes": 42.0, "styleCount": 1.0, "persisted": false}, nil
	case OpUIStorePatch:
		return map[string]any{"name": "ui", "deep": false, "patchedKeys": []any{"view"}, "changedKeys": []any{"view"}, "valuesAfter": map[string]any{"view": "settings"}, "persisted": false}, nil
	case OpUINavigate:
		return map[string]any{"view": "settings", "section": "debug", "sectionLabel": "调试模式", "persisted": false}, nil
	case OpUIWindow:
		return map[string]any{"size": map[string]any{"w": 1280.0, "h": 800.0}, "position": map[string]any{"x": 100.0, "y": 80.0},
			"maximised": false, "minimised": false, "inner": map[string]any{"w": 1280.0, "h": 769.0}}, nil
	case OpUIReload:
		return map[string]any{"scheduled": true, "delayMs": 400.0}, nil
	case OpUIClick:
		return map[string]any{"dispatched": []any{"pointerdown", "mousedown", "pointerup", "mouseup", "click"},
			"target": map[string]any{"tag": "button", "id": "", "classes": []any{"nav-item", "active"}}, "point": map[string]any{"x": 110.0, "y": 120.0}}, nil
	case OpUIType:
		return map[string]any{"dispatched": []any{"input", "change"}, "target": map[string]any{"tag": "input", "id": "masterPwd", "classes": []any{}},
			"valueAfter": "hunter2", "persisted": false}, nil
	case OpUIKey:
		return map[string]any{"dispatched": []any{"keydown", "keyup"}, "target": map[string]any{"tag": "input", "id": "masterPwd", "classes": []any{}},
			"key": "Escape", "defaultPrevented": true, "valueAfter": "hunter2"}, nil
	case OpUIHover:
		return map[string]any{"dispatched": []any{"pointerover", "mouseover", "pointerenter", "mouseenter", "pointermove", "mousemove"},
			"target": map[string]any{"tag": "button", "id": "", "classes": []any{"nav-item"}}, "point": map[string]any{"x": 110.0, "y": 120.0}}, nil
	case OpUIScroll:
		return map[string]any{"dispatched": []any{"scroll"}, "target": map[string]any{"tag": "div", "id": "", "classes": []any{"xterm-viewport"}},
			"scrollAfter": map[string]any{"scrollTop": 120.0, "scrollLeft": 0.0}, "scrollable": true}, nil
	}
	return nil, fmt.Errorf("替身前端未实现 op: %s", op)
}
