package debugsrv

// UI 观测与实验域（M5 + M6）：让 AI「看见」界面变化、安全地做实验、并量化差异。
//
// 分工（照既有模式，不另起炉灶）：
//   - 需要页面内信息的操作一律走既有前端桥：本文件 → uiFrontend → 宿主 Handler
//     （debug.go 的 Handle 把 "ui." 前缀的 op 透传给前端）→ frontend/src/debug/ui.ts；
//   - 本文件负责：工具条目与 HTTP 路由（注册见 ui_register.go）、结果裁剪与体积上限、
//     窗口截图的坐标换算与裁剪、像素/指纹差异、快照落盘、组装 MCP image 内容。
//
// 可测试性：所有「向前端要数据」都经过 uiFrontend 接口 —— 生产实现是 handlerFrontend
// （最终调用 callFrontend），测试里传替身即可在**没有 Wails 的环境**下覆盖裁剪、坐标换算、
// 像素差异、路径校验与 assert 判定（见 ui_test.go）。
//
// 能力位：读类工具注册 CapRead（永远允许）；一切会改变界面或应用状态的工具注册 CapUIWrite。
// ui.write 是可逆 / 高频操作，**不注册** RegisterToolConfirm（不需要两段式 token），
// 相关登记见 ui_register.go 的 init()。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
)

// ---- op / 工具名 ----
//
// 工具名即 op 名：MCP 工具、HTTP 路由、前端桥三处共用同一套字符串，避免多处映射失配。
const (
	OpUILayout      = "ui.layout"
	OpUIQuery       = "ui.query"
	OpUIStyles      = "ui.styles"
	OpUITokens      = "ui.tokens"
	OpUIHit         = "ui.hit"
	OpUITree        = "ui.tree"
	OpUIConsole     = "ui.console"
	OpUIRevision    = "ui.revision"
	OpUIScreenshot  = "ui.screenshot"
	OpUIElementShot = "ui.elementShot"
	OpUISnapshot    = "ui.snapshot"
	OpUIDiff        = "ui.diff"
	OpUIAssert      = "ui.assert"
	OpUIStore       = "ui.store"
	OpUISetToken    = "ui.setToken"
	OpUIInjectCSS   = "ui.injectCSS"
	OpUIStorePatch  = "ui.storePatch"
	OpUINavigate    = "ui.navigate"
	OpUIWindow      = "ui.window"
	OpUIReload      = "ui.reload"
	OpUIClick       = "ui.click"
	OpUIType        = "ui.type"
	OpUIKey         = "ui.key"
	OpUIHover       = "ui.hover"
	OpUIScroll      = "ui.scroll"
)

// 前端内部 op（不是 MCP 工具）：Go 侧只借它们取「页面内的事实」，再做裁剪 / 换算 / 判定。
const (
	// uiOpGeometry 目标元素的几何 + 视口 / 窗口外框信息（ui.screenshot / ui.elementShot / ui.snapshot 用）。
	uiOpGeometry = "ui.geometry"
	// uiOpFingerprint 可见元素指纹（ui.snapshot / ui.diff 用）。
	uiOpFingerprint = "ui.fingerprint"
	// uiOpElementFacts 单个元素的事实（ui.assert 用）。
	uiOpElementFacts = "ui.elementFacts"
	// uiOpView 当前 view / nav 状态（ui://view 资源用）。
	uiOpView = "ui.view"
)

// uiToolNames 全部 ui.* 工具（顺序 = tools/list 的展示顺序 = 能力位注册顺序）。
var uiToolNames = []string{
	OpUILayout, OpUIQuery, OpUIStyles, OpUITokens, OpUIHit, OpUITree, OpUIConsole, OpUIRevision,
	OpUIScreenshot, OpUIElementShot, OpUISnapshot, OpUIDiff, OpUIAssert, OpUIStore,
	OpUISetToken, OpUIInjectCSS, OpUIStorePatch, OpUINavigate, OpUIWindow, OpUIReload,
	OpUIClick, OpUIType, OpUIKey, OpUIHover, OpUIScroll,
}

// uiWriteTools 需要 CapUIWrite 的工具（会改变界面或应用状态）；其余 ui.* 工具按读处理。
var uiWriteTools = map[string]bool{
	OpUISetToken: true, OpUIInjectCSS: true, OpUIStorePatch: true, OpUINavigate: true,
	OpUIWindow: true, OpUIReload: true, OpUIClick: true, OpUIType: true, OpUIKey: true,
	OpUIHover: true, OpUIScroll: true,
}

// isUITool 判断工具名是否属于 UI 域（mcp.go 的分发用它把请求交给本文件处理）。
func isUITool(name string) bool {
	for _, n := range uiToolNames {
		if n == name {
			return true
		}
	}
	return false
}

// ---- 限额（所有输出都必须体积有界）----
const (
	uiMaxDataBytes    = 256 << 10 // 单次工具返回的 JSON 上限
	uiMaxStringRunes  = 200       // 元素文本默认截断长度
	uiQueryDefLimit   = 20
	uiQueryMaxLimit   = 100
	uiStylesDefLimit  = 5
	uiStylesMaxLimit  = 20
	uiTreeDefDepth    = 3
	uiTreeMaxDepth    = 6
	uiTreeDefLimit    = 60
	uiTreeMaxLimit    = 200
	uiConsoleDefLimit = 20
	uiConsoleMaxLimit = 200
	uiStoreMaxBytes   = 64 << 10
	uiCSSMaxBytes     = 64 << 10
	uiTokenMaxCount   = 200
	uiDiffTile        = 16
	uiDiffMaxRegions  = 20
	uiDiffDefThresh   = 12
	uiDiffMaxChanges  = 50
	uiOverlapMinArea  = 4
	uiSnapshotDirName = "ui-snapshots"
	uiWatchInterval   = 1500 * time.Millisecond
	// 体积兜底（uiLimitData）里的标量限制：单字段最多 400 字符、最多保留 80 个字段。
	uiLimitScalarRunes = 400
	uiLimitMaxKeys     = 80
)

// uiSnapshotNameRe 快照名白名单：字母数字开头，只允许字母数字与 . _ -，最长 64。
// 显式白名单（而不是黑名单）是防目录穿越的第一道闸门，第二道是 filepath.Base 校验。
var uiSnapshotNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ---- 返回结构 ----

// UIImage 一张要作为 MCP image 内容返回的图片（HTTP 下以 base64 出现在 JSON 里）。
type UIImage struct {
	Label    string `json:"label"`
	MimeType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
	Data     string `json:"data"`
}

// UIResult 是本域任何一次调用的返回：JSON 化的元信息 + 可选的图片内容。
//
// 为什么不直接返回 map：MCP 的 image 内容必须与文本元信息一起回给 AI，
// 统一包一层后 mcp.go 只需要一次转换（uiMCPContent），HTTP 侧也能直接序列化。
type UIResult struct {
	Data   map[string]any `json:"data"`
	Images []UIImage      `json:"images,omitempty"`
}

func uiResult(m map[string]any) *UIResult {
	if m == nil {
		m = map[string]any{}
	}
	return &UIResult{Data: m}
}

func (r *UIResult) addImage(label string, data []byte) *UIResult {
	r.Images = append(r.Images, UIImage{
		Label: label, MimeType: "image/png", Bytes: len(data),
		Data: base64.StdEncoding.EncodeToString(data),
	})
	return r
}

// ---- 前端接口（可注入）----

// uiFrontend 抽象「向前端要数据」这一件事：
//   - 生产实现 handlerFrontend：交给宿主 Handler（debugController），
//     由 debug.go 的 Handle 把 "ui." 前缀的 op 透传成前端桥调用 callFrontend；
//   - 测试实现：直接返回构造好的假数据，无需 Wails / WebView。
type uiFrontend interface {
	CallUI(ctx context.Context, op string, args map[string]any) (any, error)
}

// handlerFrontend 生产实现：把请求交给宿主 Handler（前端桥的入口）。
type handlerFrontend struct{ h Handler }

func (f handlerFrontend) CallUI(ctx context.Context, op string, args map[string]any) (any, error) {
	var raw json.RawMessage
	if len(args) > 0 {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("序列化前端参数失败: %w", err)
		}
		raw = b
	}
	v, err := f.h.Handle(ctx, op, raw)
	if err != nil {
		return nil, fmt.Errorf("前端调用失败（%s）：%w", op, err)
	}
	return v, nil
}

// uiFrontend 返回当前生效的前端入口。
func (s *Server) uiFrontend() uiFrontend {
	if s.opts.Handler == nil {
		return nil
	}
	return handlerFrontend{h: s.opts.Handler}
}

// uiFetch 向前端取一次数据，并统一要求返回对象（前端返回的非对象一律视为协议错误）。
func uiFetch(ctx context.Context, fe uiFrontend, op string, args map[string]any) (map[string]any, error) {
	if fe == nil {
		return nil, errors.New("调试服务未就绪：前端桥不可用（应用可能仍在启动）")
	}
	v, err := fe.CallUI(ctx, op, args)
	if err != nil {
		return nil, err
	}
	m := uiMap(v)
	if m == nil {
		if v == nil {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("前端返回的不是对象（op=%s）：%T", op, v)
	}
	return m, nil
}

// ---- 工具条目（由 mcp.go 集中追加，见 mcpTools）----

// uiToolEntries 返回 UI 域全部工具条目（名称 / 描述 / 入参 schema）。
//
// 描述末尾的「能力要求：…」由 mcp.go 的 withCapabilityNotes 自动追加，
// 因此这里**不要**手写能力行（手写会与真实校验脱节）。
func uiToolEntries() []mcpTool {
	const writeNote = "仅作用于当前运行实例（内存态）：刷新或 ui.reload 后还原，**不写入设置、不落库**。"
	return []mcpTool{
		// ---- A. 观测（读，永远允许）----
		{Name: OpUILayout, Description: "读取界面布局骨架：对一组关键选择器（顶栏 / 左侧导航 / SFTP 侧栏 / 终端区 / 状态栏 / 命令面板 / 快速连接面板 / " +
			"设置页容器 / 右键菜单）返回 found / visible / rect{x,y,w,h} / zIndex / overflow / pointerEvents / inViewport，" +
			"并给出可见元素之间的**非预期重叠** overlaps[]（遮挡类 bug 的候选，expected=true 表示属预期浮层）与 viewport{width,height,dpr,uiScale}。" +
			"排障首选：先看这里判断「谁在哪、有没有被盖住」。",
			InputSchema: obj(nil),
			// M9 结构化输出：形状见 data 的组装（uiLayout）。
			OutputSchema: outputObj(map[string]any{
				"viewport": outProp("object"), "view": outProp("object"),
				"entries": outProp("array"), "entryCount": outProp("number"),
				"overlaps": outProp("array"), "overlapCount": outProp("number"), "note": outProp("string"),
			}, "entries", "entryCount", "overlaps"),
			structured: structuredIdentity},
		{Name: OpUIQuery, Description: "按 CSS 选择器查询元素（默认 20 个，最多 100）：每个命中返回 {index, tag, id, classes[], text(截断 200 字), rect, visible, attrs{}, childCount}。" +
			"props 可指定要额外收集的属性名（如 [\"data-status\",\"aria-expanded\"]），默认收集 id/class/title/role/type/name/value/placeholder/data-*/aria-* 的常见项。" +
			"选择器非法或没有命中会返回中文原因。",
			InputSchema: obj(map[string]any{
				"selector": strProp("CSS 选择器（如 .xterm / aside.nav / [data-statusbar-trigger]）"),
				"limit":    intProp("返回条数（默认 20，最多 100）"),
				"props":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "额外收集的属性名列表"},
			}, "selector"),
			OutputSchema: outputObj(map[string]any{
				"selector": outProp("string"), "count": outProp("number"), "total": outProp("number"),
				"matches": outProp("array"), "truncated": outProp("boolean"), "truncatedNote": outProp("string"),
			}, "selector", "count", "matches"),
			structured: structuredIdentity},
		{Name: OpUIStyles, Description: "读取命中元素的计算样式（默认只取前 5 个，最多 20）与 cssVars{}（该元素上所有 --* 自定义属性的当前值，" +
			"用于确认设计令牌是否生效）。默认属性集：display/position/z-index/color/background-color/font*/padding/margin/width/height/" +
			"min-*/max-*/overflow/opacity/transform/transition/border-radius/box-shadow/flex/gap/visibility/pointer-events 等；props 可换成自己关心的属性。",
			InputSchema: obj(map[string]any{
				"selector": strProp("CSS 选择器"),
				"props":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要读取的 CSS 属性名（默认一组常用属性）"},
				"limit":    intProp("返回条数（默认 5，最多 20）"),
			}, "selector")},
		{Name: OpUITokens, Description: "读取设计令牌与当前主题：在**应用真正承载令牌的元素（<html>）**上读取全部 --* 自定义属性（内联 + 计算样式，" +
			"filter 可按子串过滤），并返回应用当前主题 / 预设名（mode / presetId / presetName / baseTone / 实际 data-tone）与 viewport{uiScale}。" +
			"改令牌请用 ui.setToken。",
			InputSchema: obj(map[string]any{
				"filter": strProp("只保留名称含该子串的令牌（如 --radius / --signal），可选"),
			})},
		{Name: OpUIHit, Description: "命中测试：返回该点（视口 CSS 坐标）上的元素链（document.elementsFromPoint），" +
			"每项含 {index, tag, id, classes, pointerEvents, zIndex, rect}。用于判定「谁压住了谁」——index=0 就是真正接收鼠标事件的元素。",
			InputSchema: obj(map[string]any{
				"x": intProp("视口 X（CSS px）"),
				"y": intProp("视口 Y（CSS px）"),
			}, "x", "y")},
		{Name: OpUITree, Description: "读取 Vue 组件树（从 #app 的 __vue_app__ 根实例开始）：{name, key, depth, dom{tag,id,classes}, propsSummary{}, childCount}。" +
			"depth 默认 3、最多 6；limit 默认 60、最多 200；filter 按组件名过滤。props 只做浅层摘要（函数 / 组件实例只报名字），并做循环保护，" +
			"因此不会把几 MB 的响应式对象丢回来。",
			InputSchema: obj(map[string]any{
				"depth":  intProp("最大深度（默认 3，最多 6）"),
				"limit":  intProp("最大节点数（默认 60，最多 200）"),
				"filter": strProp("只保留组件名含该子串的节点，可选"),
			})},
		{Name: OpUIConsole, Description: "读取前端控制台环形缓冲（容量 200）：{ts, level, text[], source?, stack?}。" +
			"采集 console.log/info/warn/error/debug、window.onerror 与 unhandledrejection，并保留原有控制台行为（不吞输出）。" +
			"level 按 debug < info < warn < error 取「该级别及以上」；since 为毫秒时间戳下界；limit 默认 20、最多 200。",
			InputSchema: obj(map[string]any{
				"limit": intProp("返回条数（默认 20，最多 200）"),
				"level": strProp("只保留该级别及以上：debug | info | warn | error（可选）"),
				"since": intProp("只保留 ts >= since 的条目（毫秒时间戳，可选）"),
			})},
		{Name: OpUIRevision, Description: "读取页面「代次」信息：{revision, loadedAt, timeOrigin, hotReloaded, href, viewport, view}。" +
			"revision 在每次刷新 / HMR 后自增（同一次会话内单调递增）——调用它判断「我之前看到的 DOM 认知是否已失效」；" +
			"快照与 diff 也会带上 revision，跨代次的 DOM 差异不可比。",
			InputSchema: obj(nil)},
		{Name: OpUIScreenshot, Description: "抓取窗口截图并**按元素 / 区域裁剪**后作为图片返回（受遮挡影响、窗口最小化时失败）。" +
			"selector 给元素（用它的 getBoundingClientRect），clip 给 {x,y,w,h}（视口 CSS 坐标），都不给则返回整个内容区。" +
			"scale 可缩放输出（0.05–2，最近邻重采样，用于压小体积）。返回 {rect, imageSize, viewport, scalingBasis}：" +
			"scalingBasis 明确写出坐标换算依据（截图尺寸、outer/inner 尺寸、自动标定比例、dpr、界面缩放）。",
			InputSchema: obj(map[string]any{
				"selector": strProp("要截图的元素（CSS 选择器）"),
				"clip":     uiRectSchema("裁剪区域（视口 CSS 坐标）"),
				"scale":    numProp("输出缩放（默认 1，范围 0.05–2）"),
			})},
		{Name: OpUIElementShot, Description: "ui.screenshot 的元素版：selector 必填，额外返回该元素的 rect / zIndex / classes / visible，便于" +
			"「看这个按钮现在长什么样」。",
			InputSchema: obj(map[string]any{
				"selector": strProp("要截图的元素（CSS 选择器）"),
				"scale":    numProp("输出缩放（默认 1，范围 0.05–2）"),
			}, "selector")},
		{Name: OpUISnapshot, Description: "记录一次界面快照并存档：①窗口 PNG（可用 selector / clip 裁剪）；②**指纹**（前端遍历最多 400 个可见元素，" +
			"记录 {selectorPath, tag, rect, classes, text, zIndex, visible, styles 子集}）；③元信息 {name, ts, revision, viewport}。" +
			"文件落在日志目录的 ui-snapshots/ 下（<name>.png / <name>.json），返回 {name, pngPath, jsonPath, fingerprintCount}。" +
			"典型用法：改样式前 ui.snapshot{name:'base'}，改完再 ui.snapshot{name:'after'}，然后 ui.diff。",
			InputSchema: obj(map[string]any{
				"name":     strProp("快照名（字母数字开头，仅字母数字与 . _ -，最长 64；防目录穿越）"),
				"selector": strProp("只截该元素的区域（可选）"),
				"clip":     uiRectSchema("裁剪区域（视口 CSS 坐标，可选）"),
			}, "name")},
		{Name: OpUIDiff, Description: "对比两次快照（a、b 为 ui.snapshot 的名字），返回三部分：①像素差异 {changedRatio, tiles, regions[{x,y,w,h,changedRatio}]}" +
			"（16×16 分块 + 合并相邻块，最多 20 个区域；尺寸不同则按最小公共区域裁剪并在结果里说明）并把 **diff 图**（原图叠半透明红标出变化块）" +
			"作为图片返回、同时存盘；②指纹差异 {added[], removed[], changed[{selectorPath, fields[]}]} —— fields 只报真正变化的字段：" +
			"rect / classes / zIndex / visible / text 保持 {name,from,to}，**styles 细化到 CSS 属性级**：形如 {name:\"styles\", styles:{\"border-radius\":{from,to},…}}，" +
			"只列出值真的变了的属性，一个属性都没变就不出现 styles 条目；③{revisionA, revisionB} —— 若两次快照跨了刷新 / HMR，会显式提示「期间发生过重载，DOM 差异可能不可比」。",
			InputSchema: obj(map[string]any{
				"a":         strProp("快照 A 的名字（较早）"),
				"b":         strProp("快照 B 的名字（较晚）"),
				"threshold": intProp("像素变化阈值（0–255 的通道差，默认 12；越小越敏感）"),
			}, "a", "b"),
			// M9 结构化输出：像素统计 + 指纹差异一块给（形状见 uiDiff 的 data 组装）。
			OutputSchema: outputObj(map[string]any{
				"a": outProp("string"), "b": outProp("string"), "threshold": outProp("number"),
				"changedRatio": outProp("number"), "changedPixels": outProp("number"), "totalPixels": outProp("number"),
				"changedTiles": outProp("number"), "totalTiles": outProp("number"),
				"regions": outProp("array"), "regionCount": outProp("number"),
				"fingerprint": outProp("object"),
				"pngA": outProp("string"), "pngB": outProp("string"), "diffPngPath": outProp("string"),
				"sizeMismatch": outProp("string"), "reloadWarning": outProp("string"), "note": outProp("string"),
			}, "a", "b", "changedRatio", "regions", "fingerprint"),
			structured: structuredIdentity},
		{Name: OpUIAssert, Description: "断言界面状态（用于「改完 UI 是否达标」的自动验收）：selector 必填，" +
			"可断言 exists / visible / text（完全相等）/ textContains / rectWithin{x,y,w,h}（元素矩形落在该框内）/ styles{属性: 期望值}。" +
			"返回 {passed, checks[{name, passed, expected, actual}]} —— 逐条给出期望与实际值，失败时 AI 能直接看出差在哪。",
			InputSchema: obj(map[string]any{
				"selector":     strProp("CSS 选择器"),
				"exists":       boolProp("断言元素存在"),
				"visible":      boolProp("断言元素可见"),
				"text":         strProp("断言文本完全相等（首尾空白已裁剪）"),
				"textContains": strProp("断言文本包含该子串"),
				"rectWithin":   uiRectSchema("断言元素矩形落在此框内（视口 CSS 坐标）"),
				"styles":       map[string]any{"type": "object", "description": "断言计算样式：{属性名: 期望值}，比较时忽略首尾空白与大小写"},
			}, "selector")},
		{Name: OpUIStore, Description: "读取 Pinia store 快照：不传 name 返回 store 名列表；传 name 返回该 store 的 state（JSON 安全、限制体积、" +
			"超限时截断并标注）；path 可只取子路径（如 theme / appearance.presetId）以减少体积。**只读**，不触发任何保存。",
			InputSchema: obj(map[string]any{
				"name": strProp("store 名（如 ui / settings / sessions；缺省时只返回名字列表）"),
				"path": strProp("state 内的点分路径（可选，如 appearance.presetId）"),
			})},

		// ---- B. 实验（写，需要 ui.write 能力位）----
		{Name: OpUISetToken, Description: "设置 / 移除 CSS 自定义属性（设计令牌），作用在令牌真正所在的元素（<html>）上，用于验证「换个圆角 / 换个主色会怎样」。" +
			writeNote + " 返回 {applied, removed, count}；reset 里的名字（或 tokens 里值为空串的项）表示移除该属性。",
			InputSchema: obj(map[string]any{
				"tokens": map[string]any{"type": "object", "description": "要设置的令牌：{\"--radius\":\"16px\"}；值为空串表示移除"},
				"reset":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要移除的令牌名列表"},
			})},
		{Name: OpUIInjectCSS, Description: "注入一段 <style data-ding-ssh-debug> 调试样式（id 用于覆盖 / 替换同一段；传 css:\"\" 且给 id 表示移除）。" +
			writeNote + " 适合「先试试这个改动」：比改组件代码快，也不影响设置。",
			InputSchema: obj(map[string]any{
				"css": strProp("要注入的 CSS 文本（最长 64KB）"),
				"id":  strProp("该样式段的标识（同一 id 再次注入会替换；传空 css + id 表示移除）"),
			}, "css")},
		{Name: OpUIStorePatch, Description: "对 Pinia store 执行 $patch（默认浅合并；deep=true 走函数式深合并），用于构造界面状态做实验。" +
			writeNote + " 额外提示：本工具**不会**调用任何 save()；返回里会列出受影响的字段与「这些字段是否会落库」的判断。" +
			"注意：某些 store 的方法（如 settings 设置页开关）会调用 save()，但 storePatch 本身不会触发它们。",
			InputSchema: obj(map[string]any{
				"name":  strProp("store 名（如 ui / settings / sessions）"),
				"patch": map[string]any{"type": "object", "description": "要合并进 state 的字段（或 deep=true 时的深合并对象）"},
				"deep":  boolProp("true 表示函数式深合并（$patch 的函数形式），默认 false（$patch 对象形式 = 浅合并）"),
			}, "name", "patch")},
		{Name: OpUINavigate, Description: "切换主导航视图：workspace | servers | tunnel | settings；view=settings 时可带 section" +
			"（general/theme/credentials/security/debug/logs/audit/migrate/about，通过点击设置页左侧分区实现）。" +
			"返回前会等一帧，尽量让目标处于可截图状态。" + writeNote,
			InputSchema: obj(map[string]any{
				"view":    strProp("目标视图：workspace | servers | tunnel | settings"),
				"section": strProp("设置页分区（仅 view=settings 时有效）"),
			}, "view")},
		{Name: OpUIWindow, Description: "调整应用窗口：width / height / x / y / minimize / maximize / restore，返回调整后的实际尺寸与位置。" +
			"**注意：最小化后截图（ui.screenshot / ui.snapshot）会失败**，做视觉验证前请 restore。" + writeNote,
			InputSchema: obj(map[string]any{
				"width":    intProp("窗口宽度（像素）"),
				"height":   intProp("窗口高度（像素）"),
				"x":        intProp("窗口左上角 X"),
				"y":        intProp("窗口左上角 Y"),
				"minimize": boolProp("最小化窗口（会让截图失败）"),
				"maximize": boolProp("最大化窗口"),
				"restore":  boolProp("还原窗口（取消最大化 / 取消最小化）"),
			})},
		{Name: OpUIReload, Description: "重新加载 WebView（window.runtime.WindowReload）。" +
			"**警告：会重挂整个前端、终端会重连，DOM 状态与快照指纹全部失效（ui.revision 会自增）**；" +
			"如需保留现场请先用 ui.snapshot 存档。回包在触发重载前发出，若客户端未收到回包属正常现象。" + writeNote,
			InputSchema: obj(nil)},
		{Name: OpUIClick, Description: "在元素上合成一次点击（DOM 合成事件，不做原生输入注入）：给 selector（用元素中心）或 x/y（视口 CSS 坐标）。" +
			"依次派发 pointerdown/mousedown/pointerup/mouseup/click（bubbles:true，带 clientX/clientY 与 button）。" +
			"返回 {dispatched, target{tag,id,classes}, point}；选择器找不到 / 元素不可见时返回中文原因。" + writeNote,
			InputSchema: obj(map[string]any{
				"selector":   strProp("目标元素（CSS 选择器）"),
				"x":          intProp("视口 X（CSS px，与 selector 二选一）"),
				"y":          intProp("视口 Y（CSS px）"),
				"button":     intProp("鼠标键（0 左键 / 1 中键 / 2 右键，默认 0）"),
				"clickCount": intProp("点击次数（默认 1）"),
			})},
		{Name: OpUIType, Description: "向输入框写入文本（input / textarea / contenteditable / select）：用**原生 setter + input/change 事件**绕过 Vue v-model 的缓存。" +
			"selector 缺省时用 document.activeElement。clear=true 先清空；submit=true 会 requestSubmit 最近的 form（或补一次 Enter）。" +
			"返回 {dispatched, target, valueAfter}（写入后的实际值）。" + writeNote,
			InputSchema: obj(map[string]any{
				"selector": strProp("输入元素（CSS 选择器，缺省用当前焦点元素）"),
				"text":     strProp("要写入的文本"),
				"clear":    boolProp("写入前先清空（默认 false，即追加到末尾）"),
				"submit":   boolProp("写入后提交所在表单（默认 false）"),
			}, "text")},
		{Name: OpUIKey, Description: "合成键盘事件（keydown/keypress/keyup）：key 如 Enter / Escape / ArrowDown / a；修饰键用 ctrl/shift/alt/meta。" +
			"selector 缺省时派发给当前焦点元素（无焦点则 body）。返回 {dispatched, target, valueAfter?}。" + writeNote,
			InputSchema: obj(map[string]any{
				"key":      strProp("键名（KeyboardEvent.key，如 Enter / Escape / ArrowDown / k）"),
				"ctrl":     boolProp("是否按住 Ctrl"),
				"shift":    boolProp("是否按住 Shift"),
				"alt":      boolProp("是否按住 Alt"),
				"meta":     boolProp("是否按住 Meta（⌘）"),
				"selector": strProp("目标元素（CSS 选择器，缺省用当前焦点元素）"),
			}, "key")},
		{Name: OpUIHover, Description: "把鼠标「移」到元素 / 坐标上（合成 pointerover/pointerenter/mouseover/mouseenter/mousemove），" +
			"用于触发 hover 才出现的面板或提示。返回 {dispatched, target, point}。" + writeNote,
			InputSchema: obj(map[string]any{
				"selector": strProp("目标元素（CSS 选择器）"),
				"x":        intProp("视口 X（CSS px）"),
				"y":        intProp("视口 Y（CSS px）"),
			})},
		{Name: OpUIScroll, Description: "滚动：给 selector 时滚动该元素（scrollBy/scrollTo），否则滚动窗口；dx/dy 为增量，x/y 为绝对位置" +
			"（都是 CSS px）。终端滚动用 selector=\".xterm-viewport\" 或 ui.scroll 到 .terminal-slot。" +
			"返回 {dispatched, target, scrollAfter{scrollTop,scrollLeft}}。" + writeNote,
			InputSchema: obj(map[string]any{
				"selector": strProp("要滚动的元素（CSS 选择器，缺省滚动窗口）"),
				"dx":       intProp("水平增量（CSS px）"),
				"dy":       intProp("垂直增量（CSS px）"),
				"x":        intProp("水平绝对位置"),
				"y":        intProp("垂直绝对位置"),
			})},
	}
}

// uiRectSchema 生成 {x,y,w,h} 形状的入参 schema（clip / rectWithin 共用）。
func uiRectSchema(desc string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": desc + "（缺省字段按 0 处理）",
		"properties": map[string]any{
			"x": intProp("左上角 X（CSS px）"),
			"y": intProp("左上角 Y（CSS px）"),
			"w": intProp("宽"),
			"h": intProp("高"),
		},
	}
}

// numProp 浮点参数（mcp.go 只提供了 str/int/bool 三种，scale 需要小数）。
func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

// ---- 分发 ----

// handleUIOp 是 UI 域的统一入口（mcp.go 的 tools/call 与 HTTP 路由都走它）。
func (s *Server) handleUIOp(ctx context.Context, op string, args map[string]any) (*UIResult, error) {
	return s.uiHandle(ctx, s.uiFrontend(), op, args)
}

// uiHandle 实现 UI 域的全部分发。fe 是「向前端要数据」的入口（测试可注入替身）。
func (s *Server) uiHandle(ctx context.Context, fe uiFrontend, op string, args map[string]any) (*UIResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	switch op {
	case OpUILayout:
		return s.uiLayout(ctx, fe)
	case OpUIQuery:
		return s.uiQuery(ctx, fe, args)
	case OpUIStyles:
		return s.uiStyles(ctx, fe, args)
	case OpUITokens:
		return s.uiTokens(ctx, fe, args)
	case OpUIHit:
		return s.uiHit(ctx, fe, args)
	case OpUITree:
		return s.uiTree(ctx, fe, args)
	case OpUIConsole:
		return s.uiConsole(ctx, fe, args)
	case OpUIRevision:
		return uiPassthroughTool(ctx, fe, op, args, nil)
	case OpUIScreenshot, OpUIElementShot:
		return s.uiShot(ctx, fe, op, args)
	case OpUISnapshot:
		return s.uiSnapshot(ctx, fe, args)
	case OpUIDiff:
		return s.uiDiff(args)
	case OpUIAssert:
		return s.uiAssert(ctx, fe, args)
	case OpUIStore:
		return s.uiStore(ctx, fe, args)
	case OpUISetToken, OpUIInjectCSS, OpUIStorePatch, OpUINavigate, OpUIWindow, OpUIReload,
		OpUIClick, OpUIType, OpUIKey, OpUIHover, OpUIScroll:
		return s.uiWrite(ctx, fe, op, args)
	}
	return nil, fmt.Errorf("%w: 未知的 UI 操作 %q", ErrBadInput, op)
}

// uiPassthroughTool 一次性透传（读类）：前端返回对象 → 裁剪 → 体积兜底。
func uiPassthroughTool(ctx context.Context, fe uiFrontend, op string, args map[string]any, clip func(map[string]any) map[string]any) (*UIResult, error) {
	m, err := uiFetch(ctx, fe, op, args)
	if err != nil {
		return nil, err
	}
	if clip != nil {
		m = clip(m)
	}
	return uiResult(uiLimitData(m, uiMaxDataBytes, op)), nil
}

func (s *Server) uiLayout(ctx context.Context, fe uiFrontend) (*UIResult, error) {
	m, err := uiFetch(ctx, fe, OpUILayout, nil)
	if err != nil {
		return nil, err
	}
	entries := uiCapItems(uiMapSlice(m["entries"]), 40)
	overlaps := uiLayoutOverlaps(entries)
	data := map[string]any{
		"viewport":     m["viewport"],
		"view":         m["view"],
		"entries":      uiClipValue(entries, uiMaxStringRunes, 40, 40, 0),
		"entryCount":   len(entries),
		"overlaps":     overlaps,
		"overlapCount": len(overlaps),
		"note": "overlaps 只列出「都可见、且 DOM 上互不为祖先」的重叠对（遮挡类 bug 的候选）；" +
			"expected=true 表示该重叠属预期（命令面板 / 右键菜单 / 快速连接这类浮层本来就会盖住底下的元素）。",
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUILayout)), nil
}

func (s *Server) uiQuery(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	selector := uiArgString(args, "selector")
	if selector == "" {
		return nil, fmt.Errorf("%w: 缺少 selector（例如 .xterm / aside.nav）", ErrBadInput)
	}
	limit := uiClampInt(args["limit"], uiQueryDefLimit, 1, uiQueryMaxLimit)
	m, err := uiFetch(ctx, fe, OpUIQuery, map[string]any{
		"selector": selector, "limit": limit, "props": args["props"],
	})
	if err != nil {
		return nil, err
	}
	all := uiMapSlice(m["matches"])
	matches := uiCapItems(all, limit)
	total := int(uiNum(m["total"]))
	if total < len(all) {
		total = len(all)
	}
	data := map[string]any{
		"selector": selector,
		"count":    len(matches),
		"total":    total,
		"matches":  uiClipValue(matches, uiMaxStringRunes, limit, 24, 0),
	}
	if total > len(matches) {
		data["truncated"] = true
		data["truncatedNote"] = fmt.Sprintf("共命中 %d 个元素，只返回前 %d 个（用 limit 提高，最多 %d）", total, len(matches), uiQueryMaxLimit)
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIQuery)), nil
}

// uiDefaultStyleProps 默认返回的计算样式（覆盖「遮挡 / 布局 / 视觉」三类排查最常用的属性）。
var uiDefaultStyleProps = []string{
	"display", "position", "z-index", "color", "background-color",
	"font-family", "font-size", "font-weight", "line-height", "letter-spacing",
	"padding", "margin", "width", "height", "min-width", "max-width", "min-height", "max-height",
	"overflow", "opacity", "transform", "transition", "border-radius", "box-shadow",
	"flex", "gap", "visibility", "pointer-events", "white-space", "text-align",
}

func (s *Server) uiStyles(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	selector := uiArgString(args, "selector")
	if selector == "" {
		return nil, fmt.Errorf("%w: 缺少 selector（例如 .xterm / .topbar）", ErrBadInput)
	}
	limit := uiClampInt(args["limit"], uiStylesDefLimit, 1, uiStylesMaxLimit)
	props := uiStringList(args["props"])
	if len(props) == 0 {
		props = uiDefaultStyleProps
	}
	m, err := uiFetch(ctx, fe, OpUIStyles, map[string]any{
		"selector": selector, "limit": limit, "props": props,
	})
	if err != nil {
		return nil, err
	}
	items := uiCapItems(uiMapSlice(m["items"]), limit)
	data := map[string]any{
		"selector": selector,
		"props":    uiClipValue(props, 64, 40, 10, 1),
		"count":    len(items),
		"total":    len(uiMapSlice(m["items"])),
		"items":    uiClipValue(items, uiMaxStringRunes, limit, 40, 0),
	}
	if note := uiArgString(m, "note"); note != "" {
		data["note"] = note
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIStyles)), nil
}

func (s *Server) uiTokens(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	filter := uiArgString(args, "filter")
	m, err := uiFetch(ctx, fe, OpUITokens, map[string]any{"filter": filter})
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"element":  uiArgString(m, "element"),
		"tone":     m["tone"],
		"theme":    m["theme"],
		"viewport": m["viewport"],
		"inline":   uiClipValue(m["inline"], 120, uiTokenMaxCount, uiTokenMaxCount, 1),
		"computed": uiClipValue(m["computed"], 120, uiTokenMaxCount, uiTokenMaxCount, 1),
		"count":    m["count"],
	}
	if filter != "" {
		data["filter"] = filter
	}
	if n := int(uiNum(m["count"])); n > uiTokenMaxCount {
		data["truncated"] = true
		data["truncatedNote"] = fmt.Sprintf("共 %d 个令牌，只返回前 %d 个（用 filter 过滤）", n, uiTokenMaxCount)
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUITokens)), nil
}

func (s *Server) uiHit(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	if _, okX := args["x"]; !okX {
		return nil, fmt.Errorf("%w: 缺少 x / y（视口 CSS 坐标）", ErrBadInput)
	}
	if _, okY := args["y"]; !okY {
		return nil, fmt.Errorf("%w: 缺少 y（视口 CSS 坐标）", ErrBadInput)
	}
	x, y := int(uiNum(args["x"])), int(uiNum(args["y"]))
	m, err := uiFetch(ctx, fe, OpUIHit, map[string]any{"x": x, "y": y})
	if err != nil {
		return nil, err
	}
	chain := uiCapItems(uiMapSlice(m["chain"]), 30)
	data := map[string]any{
		"x": x, "y": y,
		"chain": uiClipValue(chain, uiMaxStringRunes, 30, 20, 0),
		"count": len(chain),
		"note":  "chain[0] 是真正接收鼠标事件的元素；被遮挡时它会是遮挡者，而不是你期望的那个元素。",
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIHit)), nil
}

func (s *Server) uiTree(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	depth := uiClampInt(args["depth"], uiTreeDefDepth, 1, uiTreeMaxDepth)
	limit := uiClampInt(args["limit"], uiTreeDefLimit, 1, uiTreeMaxLimit)
	filter := uiArgString(args, "filter")
	m, err := uiFetch(ctx, fe, OpUITree, map[string]any{"depth": depth, "limit": limit, "filter": filter})
	if err != nil {
		return nil, err
	}
	allNodes := uiMapSlice(m["nodes"])
	nodes := uiCapItems(allNodes, limit)
	data := map[string]any{
		"depth": depth, "limit": limit,
		"count": len(nodes),
		"total": maxInt(int(uiNum(m["total"])), len(allNodes)),
		"nodes": uiClipValue(nodes, uiMaxStringRunes, limit, 20, 0),
	}
	if note := uiArgString(m, "note"); note != "" {
		data["note"] = note
	}
	if uiBool(m, "truncated") || len(allNodes) > len(nodes) {
		data["truncated"] = true
		data["truncatedNote"] = fmt.Sprintf("组件树超过 depth=%d / limit=%d 的限制，已截断（提高 depth/limit 可看更多）", depth, limit)
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUITree)), nil
}

// uiCapItems 在 Go 侧再兜一层数量上限。
//
// 前端理应已经按 limit 截断，但「输出体积有界」是本域的硬要求：
// 前端版本不一致 / 参数被忽略时，Go 侧必须自己也能收敛（同时让 count 与实际返回一致）。
func uiCapItems(items []map[string]any, limit int) []map[string]any {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

// uiLevelRank 把日志级别映射成可比较的序号；未知级别按 debug 处理（不会被级别过滤意外丢弃）。
func uiLevelRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return 3
	case "warn", "warning":
		return 2
	case "info", "log":
		return 1
	default:
		return 0
	}
}

// uiConsoleFilter 按 level（该级别及以上）/ since / limit 过滤控制台环形缓冲（在 Go 侧做，便于测试）。
func uiConsoleFilter(entries []map[string]any, level string, since int64, limit int) []map[string]any {
	min := uiLevelRank(level)
	hasLevel := strings.TrimSpace(level) != ""
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if since > 0 && int64(uiNum(e["ts"])) < since {
			continue
		}
		if hasLevel && uiLevelRank(uiArgString(e, "level")) < min {
			continue
		}
		out = append(out, e)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (s *Server) uiConsole(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	limit := uiClampInt(args["limit"], uiConsoleDefLimit, 1, uiConsoleMaxLimit)
	level := uiArgString(args, "level")
	since := int64(uiNum(args["since"]))
	m, err := uiFetch(ctx, fe, OpUIConsole, nil)
	if err != nil {
		return nil, err
	}
	all := uiMapSlice(m["entries"])
	kept := uiConsoleFilter(all, level, since, limit)
	data := map[string]any{
		"count":    len(kept),
		"buffered": len(all),
		"capacity": m["capacity"],
		"entries":  uiClipValue(kept, 500, limit, 20, 0),
	}
	if level != "" {
		data["level"] = level
	}
	if since > 0 {
		data["since"] = since
	}
	if len(all) > len(kept) {
		data["truncated"] = true
		data["truncatedNote"] = fmt.Sprintf("缓冲共 %d 条，按 limit/level/since 过滤后返回 %d 条", len(all), len(kept))
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIConsole)), nil
}

func (s *Server) uiStore(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	name := uiArgString(args, "name")
	path := uiArgString(args, "path")
	m, err := uiFetch(ctx, fe, OpUIStore, map[string]any{"name": name, "path": path, "maxBytes": uiStoreMaxBytes})
	if err != nil {
		return nil, err
	}
	m = uiClipValue(m, 4096, 500, 40, 0).(map[string]any)
	if note := uiArgString(m, "note"); note != "" {
		m["note"] = note
	}
	return uiResult(uiLimitData(m, uiMaxDataBytes, OpUIStore)), nil
}

// uiWrite 处理全部写类工具：交给前端执行，然后裁剪结果并统一给出「不落库」的提示。
func (s *Server) uiWrite(ctx context.Context, fe uiFrontend, op string, args map[string]any) (*UIResult, error) {
	if err := uiValidateWriteArgs(op, args); err != nil {
		return nil, err
	}
	m, err := uiFetch(ctx, fe, op, args)
	if err != nil {
		return nil, err
	}
	if op == OpUIStorePatch {
		m["persistenceHint"] = uiStorePatchHint(uiArgString(args, "name"), uiMap(args["patch"]))
	}
	if op == OpUIWindow {
		m["note"] = "最小化后截图会失败（captureWindowPNG 会返回中文错误）；做视觉验证前请先 restore。"
	}
	m = uiClipValue(m, uiMaxStringRunes*2, 100, 40, 0).(map[string]any)
	return uiResult(uiLimitData(m, uiMaxDataBytes, op)), nil
}

// uiValidateWriteArgs 写类工具的入参前置校验：把「参数写错」在做任何事之前拦下来。
func uiValidateWriteArgs(op string, args map[string]any) error {
	switch op {
	case OpUISetToken:
		if args["tokens"] == nil && args["reset"] == nil {
			return fmt.Errorf("%w: 需要 tokens（{名字:值}）或 reset（要移除的名字列表）", ErrBadInput)
		}
		for name := range uiMap(args["tokens"]) {
			if !strings.HasPrefix(name, "--") {
				return fmt.Errorf("%w: 令牌名必须以 -- 开头（收到 %q）", ErrBadInput, name)
			}
		}
		for _, name := range uiStringList(args["reset"]) {
			if !strings.HasPrefix(name, "--") {
				return fmt.Errorf("%w: 要移除的令牌名必须以 -- 开头（收到 %q）", ErrBadInput, name)
			}
		}
	case OpUIInjectCSS:
		if _, has := args["css"]; !has {
			return fmt.Errorf("%w: 缺少 css（要注入的样式文本；删除请传 css:\"\" 且给 id）", ErrBadInput)
		}
		if len(uiArgString(args, "css")) > uiCSSMaxBytes {
			return fmt.Errorf("%w: css 超过 %d 字节上限", ErrBadInput, uiCSSMaxBytes)
		}
		if uiArgString(args, "css") == "" && uiArgString(args, "id") == "" {
			return fmt.Errorf("%w: css 为空时必须给 id（表示移除该样式段）", ErrBadInput)
		}
	case OpUIStorePatch:
		if uiArgString(args, "name") == "" {
			return fmt.Errorf("%w: 缺少 name（要改的 store 名）", ErrBadInput)
		}
		if uiMap(args["patch"]) == nil {
			return fmt.Errorf("%w: 缺少 patch（要合并进 state 的字段对象）", ErrBadInput)
		}
	case OpUINavigate:
		switch uiArgString(args, "view") {
		case "workspace", "servers", "tunnel", "settings":
		default:
			return fmt.Errorf("%w: view 必须是 workspace | servers | tunnel | settings（收到 %q）", ErrBadInput, uiArgString(args, "view"))
		}
	case OpUIType:
		if _, has := args["text"]; !has {
			return fmt.Errorf("%w: 缺少 text", ErrBadInput)
		}
	case OpUIKey:
		if uiArgString(args, "key") == "" {
			return fmt.Errorf("%w: 缺少 key（如 Enter / Escape / ArrowDown）", ErrBadInput)
		}
	}
	return nil
}

// uiStorePatchHint 给出「这些字段会不会落库」的中文判断，避免 AI 以为 storePatch 改了设置。
func uiStorePatchHint(name string, patch map[string]any) string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	base := "本工具不会调用任何 save()/持久化：改动只存在于内存（刷新即还原）。受影响字段：" + strings.Join(keys, "、") + "。"
	switch name {
	case "settings":
		return base + "settings store 的字段只有在调用其 set*/save() 动作时才会写库（设置页的开关会调用），因此本次 patch 不会落库；" +
			"但界面可能已按新值重绘（例如 uiScale / theme / appearance），需要还原请再 patch 回去或刷新页面。"
	case "sessions", "ui":
		return base + "该 store 是纯界面状态，不会被持久化。"
	case "servers", "credentials", "groups":
		return base + "该 store 的 save() 会落库，但 storePatch 不调用它；刷新后仍从存储读取原值。"
	default:
		return base + "若该 store 的某个 watcher 或动作会写库，请自行确认（本工具不触发它们）。"
	}
}

// ---- 数值 / 结构工具函数 ----

func uiMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// uiMapSlice 把 []any 里的对象项取出来（非对象项忽略）。
func uiMapSlice(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		if ms, isMS := v.([]map[string]any); isMS {
			return ms
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m := uiMap(it); m != nil {
			out = append(out, m)
		}
	}
	return out
}

func uiArgString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func uiNum(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func uiBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

// uiClampInt 从任意 JSON 值取整数并按 [lo,hi] 收敛；缺省时用 def。
func uiClampInt(v any, def, lo, hi int) int {
	n := def
	if v != nil {
		n = int(math.Round(uiNum(v)))
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// uiStringList 从任意 JSON 值取字符串列表（忽略非字符串项与空串）。
func uiStringList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		if ss, isSS := v.([]string); isSS {
			return ss
		}
		if s, isS := v.(string); isS && strings.TrimSpace(s) != "" {
			return []string{strings.TrimSpace(s)}
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, isS := it.(string); isS && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// uiRect 视口坐标系（CSS px）下的矩形。
type uiRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// uiRectFromAny 解析 {x,y,w,h}；w/h 缺失或非正时返回 false。
func uiRectFromAny(v any) (uiRect, bool) {
	m := uiMap(v)
	if m == nil {
		return uiRect{}, false
	}
	r := uiRect{
		X: int(math.Round(uiNum(m["x"]))),
		Y: int(math.Round(uiNum(m["y"]))),
		W: int(math.Round(uiNum(m["w"]))),
		H: int(math.Round(uiNum(m["h"]))),
	}
	if r.W <= 0 || r.H <= 0 {
		return r, false
	}
	return r, true
}

// uiIntersectArea 返回两个矩形的交集面积（无交集为 0）与交集矩形。
func uiIntersectArea(a, b uiRect) (uiRect, int) {
	x0 := maxInt(a.X, b.X)
	y0 := maxInt(a.Y, b.Y)
	x1 := minInt(a.X+a.W, b.X+b.W)
	y1 := minInt(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return uiRect{}, 0
	}
	r := uiRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	return r, r.W * r.H
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// uiLayoutOverlaps 从布局条目里挑出**非预期**的可见重叠对。
//
// 判定：
//   - 双方都 visible（且 inViewport 不为 false）；
//   - DOM 上互不为祖先（前端在 contains 里给出了「我包含的其它 key」）；
//   - 交集面积 > uiOverlapMinArea；
//   - 浮层（浮层标记 overlay=true，如命令面板 / 右键菜单 / 快速连接）参与的重叠标 expected=true。
func uiLayoutOverlaps(entries []map[string]any) []any {
	type pair struct {
		item map[string]any
		area int
	}
	var pairs []pair
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if !uiEntryVisible(a) || !uiEntryVisible(b) {
				continue
			}
			if uiEntryContains(a, uiArgString(b, "name")) || uiEntryContains(b, uiArgString(a, "name")) {
				continue // 祖先 / 后代关系：重叠是结构使然，不算 bug
			}
			ra, okA := uiRectFromAny(a["rect"])
			rb, okB := uiRectFromAny(b["rect"])
			if !okA || !okB {
				continue
			}
			inter, area := uiIntersectArea(ra, rb)
			if area <= uiOverlapMinArea {
				continue
			}
			expected := uiBool(a, "overlay") || uiBool(b, "overlay")
			reason := fmt.Sprintf("「%s」与「%s」在视口内互相重叠（交集 %dx%d），可能互相遮挡：请用 ui.hit 确认谁在最上层。",
				uiArgString(a, "name"), uiArgString(b, "name"), inter.W, inter.H)
			if expected {
				reason = fmt.Sprintf("「%s」与「%s」重叠，但其中一方是浮层（命令面板 / 右键菜单 / 快速连接），属预期遮挡。",
					uiArgString(a, "name"), uiArgString(b, "name"))
			}
			pairs = append(pairs, pair{item: map[string]any{
				"a": uiArgString(a, "name"), "b": uiArgString(b, "name"),
				"area": area, "rect": inter, "expected": expected, "reason": reason,
			}, area: area})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].area > pairs[j].area })
	out := make([]any, 0, len(pairs))
	for i, p := range pairs {
		if i >= uiDiffMaxRegions {
			out = append(out, map[string]any{
				"truncated": true,
				"truncatedNote": fmt.Sprintf("还有 %d 对重叠未列出（按交集面积排序，只保留前 %d 对）",
					len(pairs)-uiDiffMaxRegions, uiDiffMaxRegions),
			})
			break
		}
		out = append(out, p.item)
	}
	return out
}

// uiEntryVisible 布局条目的「真的看得见」判定（inViewport 缺省视为 true）。
func uiEntryVisible(e map[string]any) bool {
	if !uiBool(e, "visible") {
		return false
	}
	if _, has := e["inViewport"]; has {
		return uiBool(e, "inViewport")
	}
	return true
}

// uiEntryContains 判断布局条目 a 的 contains 列表里是否含 key。
func uiEntryContains(a map[string]any, key string) bool {
	if key == "" {
		return false
	}
	items, ok := a["contains"].([]any)
	if !ok {
		return false
	}
	for _, it := range items {
		if s, isS := it.(string); isS && s == key {
			return true
		}
	}
	return false
}

// ---- 裁剪与体积上限 ----

// uiClipString 按 rune 截断字符串，并带上明确的截断标记。
func uiClipString(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + fmt.Sprintf("…（已截断 %d 字符）", len(runes)-maxRunes)
}

// uiClipValue 递归裁剪任意 JSON 值：
//   - 字符串超过 maxRunes 时截断并标记；
//   - 数组超过 maxItems 时保留前 maxItems 项，并追加一条截断说明；
//   - 对象超过 maxKeys 时按键名排序保留前 maxKeys 个（顺序稳定，便于 diff），并记录被丢弃的键数；
//   - 嵌套超过 depth 上限时用形状描述替代（避免把整棵响应式对象丢回来）。
func uiClipValue(v any, maxRunes, maxItems, maxKeys, depth int) any {
	if depth > 4 {
		return uiShapeOf(v)
	}
	switch t := v.(type) {
	case nil, bool:
		return v
	case string:
		return uiClipString(t, maxRunes)
	case float64, int, int64, json.Number:
		return v
	case []any:
		limit := maxItems
		if limit <= 0 {
			limit = len(t)
		}
		out := make([]any, 0, minInt(len(t), limit)+1)
		for i, it := range t {
			if i >= limit {
				out = append(out, map[string]any{
					"truncated":     true,
					"truncatedNote": fmt.Sprintf("还有 %d 项未列出", len(t)-limit),
				})
				break
			}
			out = append(out, uiClipValue(it, maxRunes, maxItems, maxKeys, depth+1))
		}
		return out
	case []map[string]any:
		// Go 侧自己构造的切片（例如 uiCapItems 的结果）：转成 []any 后走同一套裁剪逻辑。
		items := make([]any, 0, len(t))
		for _, it := range t {
			items = append(items, it)
		}
		return uiClipValue(items, maxRunes, maxItems, maxKeys, depth)
	case []string:
		items := make([]any, 0, len(t))
		for _, it := range t {
			items = append(items, it)
		}
		return uiClipValue(items, maxRunes, maxItems, maxKeys, depth)
	case map[string]any:
		if maxKeys > 0 && len(t) > maxKeys {
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out := make(map[string]any, maxKeys+2)
			for _, k := range keys[:maxKeys] {
				out[k] = uiClipValue(t[k], maxRunes, maxItems, maxKeys, depth+1)
			}
			out["truncated"] = true
			out["truncatedNote"] = fmt.Sprintf("对象键过多（共 %d 个），已按键名排序只保留前 %d 个", len(t), maxKeys)
			return out
		}
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = uiClipValue(val, maxRunes, maxItems, maxKeys, depth+1)
		}
		return out
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return uiShapeOf(v)
		}
		return uiClipString(string(b), maxRunes)
	}
}

// uiShapeOf 用一句话描述一个复杂值的形状（体积兜底时替代字段值）。
func uiShapeOf(v any) string {
	switch t := v.(type) {
	case []any:
		return fmt.Sprintf("已省略数组（%d 项）", len(t))
	case map[string]any:
		return fmt.Sprintf("已省略对象（%d 个键）", len(t))
	case nil:
		return ""
	default:
		return uiClipString(fmt.Sprintf("已省略 %T", v), 64)
	}
}

// uiLimitData 体积兜底：超过 maxBytes 时保留标量字段（并截断超长字符串、限制字段数），
// 把复杂字段替换为形状描述，并显式标注截断原因。
//
// 为什么需要它：任何一处遗漏的 limit 都可能把几 MB 的 DOM / store 快照丢回 MCP，
// 因此每个 ui.* 工具的返回都过一遍这里。注意「标量也要截断」——单个字段就可能是几 MB
// 的文本（例如把整个 state 塞进一个字符串）。
func uiLimitData(m map[string]any, maxBytes int, tool string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(m)
	if err == nil && len(raw) <= maxBytes {
		return m
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, uiLimitMaxKeys+4)
	dropped := 0
	for _, k := range keys {
		if len(out) >= uiLimitMaxKeys {
			dropped++
			continue
		}
		switch v := m[k].(type) {
		case nil, bool, float64, int, int64:
			out[k] = v
		case string:
			out[k] = uiClipString(v, uiLimitScalarRunes)
		default:
			out[k] = uiShapeOf(v)
		}
	}
	size := 0
	if err == nil {
		size = len(raw)
	}
	out["truncated"] = true
	out["tool"] = tool
	out["truncatedNote"] = fmt.Sprintf("结果超过 %d 字节上限（实际 %d 字节），已丢弃非标量字段、截断超长文本"+
		"（保留 %d 个字段%s）；请用更小的 limit / props 或 ui.query 缩小范围后重试。",
		maxBytes, size, len(out), uiDroppedNote(dropped))
	// 最后一道保险：极端情况下（例如大量超长字段名）仍超限时只回一条说明。
	if b, err := json.Marshal(out); err == nil && len(b) > 2*maxBytes {
		return map[string]any{
			"truncated":     true,
			"tool":          tool,
			"truncatedNote": fmt.Sprintf("结果过大（%d 字节 > %d 字节上限），已全部丢弃；请用更小的 limit / props 重试。", len(b), maxBytes),
		}
	}
	return out
}

func uiDroppedNote(dropped int) string {
	if dropped <= 0 {
		return ""
	}
	return fmt.Sprintf("，另有 %d 个字段被丢弃", dropped)
}

// ---- MCP 内容组装 ----

// uiMCPContent 把 UI 域 handler 的返回组装成 MCP 的 content 数组：文本元信息 + 若干 image。
func uiMCPContent(v any) ([]any, error) {
	res, ok := v.(*UIResult)
	if !ok {
		// 兜底：不是本域的结果（例如替身 Handler 直接回了个 map）→ 按普通文本返回
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"type": "text", "text": string(b)}}, nil
	}
	data := res.Data
	if data == nil {
		data = map[string]any{}
	}
	if len(res.Images) > 0 {
		labels := make([]any, 0, len(res.Images))
		for i, img := range res.Images {
			labels = append(labels, map[string]any{
				"index": i, "label": img.Label, "mimeType": img.MimeType, "bytes": img.Bytes,
				"note": fmt.Sprintf("本图片是 content[%d] 的 image 内容（base64 未放进文本）", i+1),
			})
		}
		data["images"] = labels
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	out := []any{map[string]any{"type": "text", "text": string(b)}}
	for _, img := range res.Images {
		out = append(out, map[string]any{
			"type": "image", "data": img.Data, "mimeType": img.MimeType,
		})
	}
	return out, nil
}

// ---- 快照：名称 / 路径 / 读写 ----

// uiSnapshotDir 快照目录：日志目录下的 ui-snapshots/。
func uiSnapshotDir() string {
	return filepath.Join(logx.LogDir(), uiSnapshotDirName)
}

// validateUISnapshotName 校验快照名，返回规范化后的名字。
//
// 双重闸门：字符白名单 + filepath.Base（拒绝任何路径分隔符与 ..），
// 保证 ai 传进来的名字永远只能落在 ui-snapshots/ 里。
func validateUISnapshotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: 缺少快照名 name（只允许字母数字与 . _ -，最长 64）", ErrBadInput)
	}
	if !uiSnapshotNameRe.MatchString(name) {
		return "", fmt.Errorf("%w: 快照名 %q 非法：只允许字母数字开头、字母数字与 . _ - 组成、最长 64 个字符（不允许路径分隔符）", ErrBadInput, name)
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("%w: 快照名 %q 不能包含路径成分（防目录穿越）", ErrBadInput, name)
	}
	return name, nil
}

// uiSnapshotPaths 返回快照的 PNG 与元信息路径（已校验，必定落在 ui-snapshots/ 下）。
func uiSnapshotPaths(name string) (pngPath, metaPath string, err error) {
	clean, err := validateUISnapshotName(name)
	if err != nil {
		return "", "", err
	}
	dir := uiSnapshotDir()
	pngPath = filepath.Join(dir, clean+".png")
	metaPath = filepath.Join(dir, clean+".json")
	// 再确认一次：拼接结果必须仍在快照目录内（防御性地把校验写成断言）。
	for _, p := range []string{pngPath, metaPath} {
		if filepath.Dir(p) != filepath.Clean(dir) {
			return "", "", fmt.Errorf("%w: 快照路径越界（%s）", ErrBadInput, p)
		}
	}
	return pngPath, metaPath, nil
}

// uiSnapshotMeta 快照元信息（<name>.json）：既供 ui.diff 读指纹，也供人查看本次快照的上下文。
type uiSnapshotMeta struct {
	Name        string           `json:"name"`
	TS          int64            `json:"ts"`
	Revision    any              `json:"revision"`
	View        any              `json:"view"`
	Viewport    any              `json:"viewport"`
	Selector    string           `json:"selector,omitempty"`
	Clip        *uiRect          `json:"clip,omitempty"`
	ImageSize   map[string]int   `json:"imageSize"`
	PNGPath     string           `json:"pngPath"`
	Fingerprint []map[string]any `json:"fingerprint"`
	Truncated   bool             `json:"fingerprintTruncated"`
}

// uiLoadSnapshot 读取快照元信息；不存在时给出中文错误。
func uiLoadSnapshot(name string) (*uiSnapshotMeta, error) {
	_, metaPath, err := uiSnapshotPaths(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: 快照 %q 不存在（请先用 ui.snapshot 记录；路径：%s）", ErrNotFound, name, metaPath)
		}
		return nil, fmt.Errorf("读取快照元信息失败（%s）：%w", metaPath, err)
	}
	var meta uiSnapshotMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, fmt.Errorf("快照元信息损坏（%s）：%w", metaPath, err)
	}
	return &meta, nil
}

// uiLoadSnapshotPNG 读取快照 PNG 并解码。
func uiLoadSnapshotPNG(name string) (image.Image, string, error) {
	pngPath, _, err := uiSnapshotPaths(name)
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(pngPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("%w: 快照 %q 的 PNG 不存在（%s）", ErrNotFound, name, pngPath)
		}
		return nil, "", fmt.Errorf("读取快照 PNG 失败（%s）：%w", pngPath, err)
	}
	img, err := uiDecodePNG(b)
	if err != nil {
		return nil, "", fmt.Errorf("解码快照 PNG 失败（%s）：%w", pngPath, err)
	}
	return img, pngPath, nil
}

func uiDecodePNG(b []byte) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, errors.New("不是合法的 PNG（窗口截图失败或文件损坏）")
	}
	return img, nil
}

// uiWriteFile 落盘（自动建目录）。
func uiWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败（%s）：%w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入文件失败（%s）：%w", path, err)
	}
	return nil
}

func uiEncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("PNG 编码失败：%w", err)
	}
	return buf.Bytes(), nil
}

// ---- 截图：坐标换算 / 裁剪 / 缩放 ----

// uiScaleCalc 是一次「前端 CSS 坐标 → 截图像素」换算的全部依据。
//
// 换算规则（也是返回给 AI 的 scalingBasis 文案内容）：
//   - 前端给的是 getBoundingClientRect()（视口 CSS 坐标，**已包含** .app-shell 的
//     zoom: uiScale% —— 因此这里不再额外乘界面缩放系数）；
//   - 截图是窗口**外框**（GetWindowRect）的物理像素，内容区相对外框有标题栏 / 边框偏移
//     （用前端 window.outerWidth/Height 与 innerWidth/Height 算出来）；
//   - 每 CSS px 对应多少截图 px 用「实际截图宽度 ÷ 窗口外框 CSS 宽度」**自动标定**，
//     不盲信 devicePixelRatio（WebView2 的 dpr、显示缩放、DPI 感知模式都可能让它们不一致）；
//   - 两者偏差超过 2% 时置 Drift=true，并在结果里提示「换算可能有偏差」。
type uiScaleCalc struct {
	ImageW, ImageH   int
	OuterW, OuterH   float64
	InnerW, InnerH   float64
	ChromeX, ChromeY float64
	ScaleX, ScaleY   float64
	DPR              float64
	UIScale          float64
	Drift            bool
}

// uiComputeScale 根据截图实际尺寸与前端给的窗口几何，算出换算参数。
//
// 三种确定「内容区相对窗口外框的偏移」的来源，按可靠性排序：
//  1. 浏览器如实报告的外框与内容区之差（outer > inner）；标题栏 = 高度差 − 单侧边框宽；
//  2. WebView2 的实际情况：window.outerWidth/Height 与 innerWidth/Height 相同（都是内容区），
//     此时用「截图尺寸 ÷ devicePixelRatio − 视口尺寸」反推：左右边框对称、
//     底部边框与侧面等宽 → 单侧边框 = (内容区宽度差)/2，标题栏 = 高度差 − 单侧边框；
//  3. 都拿不到时按 0 处理（并在结果里标注 Drift，提示换算可能有偏差）。
func uiComputeScale(imgW, imgH int, geo map[string]any) uiScaleCalc {
	outer := uiMap(geo["outer"])
	inner := uiMap(geo["inner"])
	vp := uiMap(geo["viewport"])
	c := uiScaleCalc{
		ImageW: imgW, ImageH: imgH,
		OuterW: uiNum(outer["w"]), OuterH: uiNum(outer["h"]),
		InnerW: uiNum(inner["w"]), InnerH: uiNum(inner["h"]),
		DPR: uiNum(vp["dpr"]), UIScale: uiNum(vp["uiScale"]),
	}
	if c.DPR <= 0 {
		c.DPR = 1
	}
	// 界面缩放：前端可能给分数（1 = 100%）也可能给百分比（100 = 100%），这里统一成分数。
	if c.UIScale > 5 {
		c.UIScale = c.UIScale / 100
	}
	if c.UIScale <= 0 {
		c.UIScale = 1
	}
	// 视口尺寸（CSS px）就是内容区尺寸：它比 window.innerWidth 更接近「应用真正画的区域」，
	// 且一定与 getBoundingClientRect() 同一坐标系。
	innerW, innerH := uiNum(vp["width"]), uiNum(vp["height"])
	if innerW <= 0 {
		innerW = c.InnerW
	}
	if innerH <= 0 {
		innerH = c.InnerH
	}
	reliable := true
	switch {
	case c.OuterW > innerW && innerW > 0:
		c.ChromeX = (c.OuterW - innerW) / 2
	case innerW > 0:
		// 用截图反推（WebView2：outer == inner == 内容区）
		if derived := (float64(imgW)/c.DPR - innerW) / 2; derived > 0 {
			c.ChromeX = derived
		}
	}
	switch {
	case c.OuterH > innerH && innerH > 0:
		c.ChromeY = (c.OuterH - innerH) - c.ChromeX
	case innerH > 0:
		if derived := float64(imgH)/c.DPR - innerH - c.ChromeX; derived > 0 {
			c.ChromeY = derived
		}
	}
	// 反推结果超出合理范围（窗口边框 0–64 CSS px）说明前提不成立：退回 0 并标记不可靠。
	if c.ChromeX < 0 || c.ChromeX > 64 || c.ChromeY < 0 || c.ChromeY > 96 {
		c.ChromeX, c.ChromeY, reliable = 0, 0, false
	}
	// 标定：内容区 + 边框 = 截图对应的 CSS 尺寸（等价于窗口外框尺寸）。
	contentW := innerW + 2*c.ChromeX
	contentH := innerH + c.ChromeX + c.ChromeY
	if contentW > 0 {
		c.ScaleX = float64(imgW) / contentW
	} else {
		c.ScaleX = c.DPR
	}
	if contentH > 0 {
		c.ScaleY = float64(imgH) / contentH
	} else {
		c.ScaleY = c.DPR
	}
	if c.ScaleX <= 0 {
		c.ScaleX = 1
	}
	if c.ScaleY <= 0 {
		c.ScaleY = 1
	}
	c.Drift = !reliable || math.Abs(c.ScaleX-c.ScaleY) > 0.02 || math.Abs(c.ScaleX-c.DPR) > 0.02
	return c
}

// imageRect 把视口 CSS 坐标的矩形换算成截图像素矩形（并裁剪到图片范围内）。
func (c uiScaleCalc) imageRect(r uiRect) image.Rectangle {
	x0 := int(math.Round((float64(r.X) + c.ChromeX) * c.ScaleX))
	y0 := int(math.Round((float64(r.Y) + c.ChromeY) * c.ScaleY))
	x1 := int(math.Round((float64(r.X+r.W) + c.ChromeX) * c.ScaleX))
	y1 := int(math.Round((float64(r.Y+r.H) + c.ChromeY) * c.ScaleY))
	return image.Rect(x0, y0, x1, y1).Intersect(image.Rect(0, 0, c.ImageW, c.ImageH))
}

// basis 返回中文的换算依据（写进工具返回，AI 与人都能复核）。
func (c uiScaleCalc) basis() string {
	drift := ""
	if c.Drift {
		drift = "；⚠ 换算依据不完整（自动标定比例与 dpr 偏差 > 2%，或窗口边框反推超出合理范围），" +
			"裁剪结果可能有若干像素偏移，请以返回的 rect / pixelRect 为准"
	}
	return fmt.Sprintf(
		"换算依据：截图 %dx%d px；内容区（视口）%.0fx%.0f CSS px；窗口外框 CSS 尺寸 %.0fx%.0f（0 表示浏览器未如实上报，改由截图反推）；"+
			"内容区相对窗口外框的偏移 x=%.1f y=%.1f CSS px（含标题栏与边框）；"+
			"每 CSS px 对应截图 px 由「截图尺寸 ÷（内容区 + 边框）」自动标定 x=%.4f y=%.4f（devicePixelRatio=%.2f，界面缩放=%.0f%%）；"+
			"元素坐标来自 getBoundingClientRect()，已包含界面缩放，故不再额外乘缩放系数%s",
		c.ImageW, c.ImageH, c.InnerW, c.InnerH, c.OuterW, c.OuterH, c.ChromeX, c.ChromeY,
		c.ScaleX, c.ScaleY, c.DPR, c.UIScale*100, drift)
}

// uiShotTarget 从前端几何信息里选出实际要截的区域（优先级：clip > 元素 rect > 整个内容区）。
func uiShotTarget(geo map[string]any, args map[string]any) (uiRect, string, error) {
	if clip, ok := uiRectFromAny(args["clip"]); ok {
		return clip, "clip", nil
	}
	if clip, ok := uiRectFromAny(geo["clip"]); ok {
		return clip, "clip", nil
	}
	if r, ok := uiRectFromAny(geo["rect"]); ok {
		return r, "element", nil
	}
	vp := uiMap(geo["viewport"])
	w, h := int(uiNum(vp["width"])), int(uiNum(vp["height"]))
	if w <= 0 || h <= 0 {
		return uiRect{}, "", errors.New("无法确定截图区域：前端未返回元素矩形，也没有可用的视口尺寸")
	}
	return uiRect{X: 0, Y: 0, W: w, H: h}, "viewport", nil
}

// uiCropImage 裁剪（超出边界部分先做交集）。
func uiCropImage(src image.Image, r image.Rectangle) image.Image {
	r = r.Intersect(src.Bounds())
	if r.Empty() {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	return dst
}

// uiResampleNearest 最近邻缩放（不引入新依赖）。
//
// 为什么不用更好的插值：本工具的用途是「看界面 / 比对差异」，最近邻不改变颜色分布，
// 因此不会让像素差异误报；同时也最省内存。
func uiResampleNearest(src image.Image, scale float64) image.Image {
	b := src.Bounds()
	w := int(math.Round(float64(b.Dx()) * scale))
	h := int(math.Round(float64(b.Dy()) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w == b.Dx() && h == b.Dy() {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// uiScaleFromArgs 读取 scale 参数（默认 1，范围 0.05–2）。
func uiScaleFromArgs(args map[string]any) float64 {
	s := 1.0
	if v, has := args["scale"]; has && v != nil {
		s = uiNum(v)
	}
	if s <= 0 {
		s = 1
	}
	return math.Max(0.05, math.Min(2, s))
}

// uiShot 实现 ui.screenshot / ui.elementShot。
func (s *Server) uiShot(ctx context.Context, fe uiFrontend, op string, args map[string]any) (*UIResult, error) {
	selector := uiArgString(args, "selector")
	if op == OpUIElementShot && selector == "" {
		return nil, fmt.Errorf("%w: ui.elementShot 必须给 selector（要截图的元素）", ErrBadInput)
	}
	geo, err := uiFetch(ctx, fe, uiOpGeometry, map[string]any{
		"selector": selector, "clip": args["clip"],
	})
	if err != nil {
		return nil, err
	}
	if selector != "" && !uiBool(geo, "found") {
		return nil, fmt.Errorf("%w: 未找到选择器 %q（元素不在 DOM 中）", ErrNotFound, selector)
	}
	if selector != "" && !uiBool(geo, "visible") {
		return nil, fmt.Errorf("%w: 元素 %q 不可见（display:none / 尺寸为 0 / 被隐藏），无法截图", ErrBadInput, selector)
	}
	rect, source, err := uiShotTarget(geo, args)
	if err != nil {
		return nil, err
	}
	rawPNG, err := captureWindowPNG()
	if err != nil {
		return nil, fmt.Errorf("窗口截图失败：%w（提示：窗口最小化或被完全遮挡时无法截图，请先 ui.window{restore:true}）", err)
	}
	img, err := uiDecodePNG(rawPNG)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	calc := uiComputeScale(bounds.Dx(), bounds.Dy(), geo)
	crop := calc.imageRect(rect)
	if crop.Empty() {
		return nil, fmt.Errorf("%w: 裁剪区域为空（目标 rect=%+v 超出窗口截图范围 %dx%d）", ErrBadInput, rect, bounds.Dx(), bounds.Dy())
	}
	out := uiCropImage(img, crop)
	scale := uiScaleFromArgs(args)
	if scale != 1 {
		out = uiResampleNearest(out, scale)
	}
	pngBytes, err := uiEncodePNG(out)
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"selector":     selector,
		"source":       source,
		"rect":         rect,
		"imageSize":    map[string]int{"w": out.Bounds().Dx(), "h": out.Bounds().Dy()},
		"pixelRect":    map[string]int{"x": crop.Min.X, "y": crop.Min.Y, "w": crop.Dx(), "h": crop.Dy()},
		"viewport":     geo["viewport"],
		"outer":        geo["outer"],
		"inner":        geo["inner"],
		"dpr":          calc.DPR,
		"uiScale":      calc.UIScale,
		"autoScale":    map[string]float64{"x": calc.ScaleX, "y": calc.ScaleY},
		"scale":        scale,
		"scalingBasis": calc.basis(),
		"imageBytes":   len(pngBytes),
	}
	if op == OpUIElementShot {
		data["classes"] = geo["classes"]
		data["zIndex"] = geo["zIndex"]
		data["visible"] = geo["visible"]
		data["tag"] = geo["tag"]
	}
	if calc.Drift {
		data["scalingWarning"] = "自动标定比例与 devicePixelRatio 不一致（可能因 DPI 缩放 / 窗口边框），裁剪可能有 1~2px 偏移"
	}
	res := uiResult(uiLimitData(data, uiMaxDataBytes, op))
	return res.addImage("窗口截图裁剪（"+source+"）", pngBytes), nil
}

// ---- 快照 / 差异 ----

// uiSnapshot 记录一次快照：窗口 PNG（可裁剪）+ 指纹 + 元信息，落盘到 ui-snapshots/。
func (s *Server) uiSnapshot(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	name, err := validateUISnapshotName(uiArgString(args, "name"))
	if err != nil {
		return nil, err
	}
	selector := uiArgString(args, "selector")
	pngPath, metaPath, err := uiSnapshotPaths(name)
	if err != nil {
		return nil, err
	}

	// 指纹（页面内事实）
	fp, err := uiFetch(ctx, fe, uiOpFingerprint, map[string]any{"selector": selector})
	if err != nil {
		return nil, err
	}
	fingerprint := uiMapSlice(fp["fingerprint"])

	// 几何（窗口外框 / 内容区 / 视口）：换算与裁剪都必须有它，否则会带上标题栏。
	geo, err := uiFetch(ctx, fe, uiOpGeometry, map[string]any{"selector": selector, "clip": args["clip"]})
	if err != nil {
		return nil, err
	}
	if selector != "" && !uiBool(geo, "found") {
		return nil, fmt.Errorf("%w: 未找到选择器 %q（元素不在 DOM 中）", ErrNotFound, selector)
	}
	if selector != "" && !uiBool(geo, "visible") {
		return nil, fmt.Errorf("%w: 元素 %q 不可见（display:none / 尺寸为 0），无法截图", ErrBadInput, selector)
	}
	rect, source, err := uiShotTarget(geo, args)
	if err != nil {
		return nil, err
	}
	clipRect := &rect
	viewport := geo["viewport"]
	if viewport == nil {
		viewport = fp["viewport"]
	}

	rawPNG, err := captureWindowPNG()
	if err != nil {
		return nil, fmt.Errorf("窗口截图失败：%w（提示：窗口最小化时无法截图，请先 ui.window{restore:true}）", err)
	}
	img, err := uiDecodePNG(rawPNG)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	calc := uiComputeScale(bounds.Dx(), bounds.Dy(), geo)
	crop := calc.imageRect(*clipRect)
	if crop.Empty() {
		return nil, fmt.Errorf("%w: 裁剪区域为空（目标 rect=%+v 超出窗口截图范围 %dx%d）", ErrBadInput, *clipRect, bounds.Dx(), bounds.Dy())
	}
	out := uiCropImage(img, crop)
	pngBytes, err := uiEncodePNG(out)
	if err != nil {
		return nil, err
	}
	if err := uiWriteFile(pngPath, pngBytes); err != nil {
		return nil, err
	}
	meta := uiSnapshotMeta{
		Name: name, TS: time.Now().UnixMilli(),
		Revision: fp["revision"], View: fp["view"], Viewport: viewport,
		Selector: selector, Clip: clipRect,
		ImageSize:   map[string]int{"w": out.Bounds().Dx(), "h": out.Bounds().Dy()},
		PNGPath:     pngPath,
		Fingerprint: fingerprint,
		Truncated:   uiBool(fp, "truncated"),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := uiWriteFile(metaPath, metaBytes); err != nil {
		return nil, err
	}
	data := map[string]any{
		"name":                 name,
		"pngPath":              pngPath,
		"jsonPath":             metaPath,
		"fingerprintCount":     len(fingerprint),
		"imageSize":            meta.ImageSize,
		"pixelRect":            map[string]int{"x": crop.Min.X, "y": crop.Min.Y, "w": crop.Dx(), "h": crop.Dy()},
		"revision":             fp["revision"],
		"viewport":             viewport,
		"view":                 fp["view"],
		"clip":                 clipRect,
		"source":               source,
		"ts":                   meta.TS,
		"fingerprintTruncated": meta.Truncated,
		"scalingBasis":         calc.basis(),
		"snapshotDir":          uiSnapshotDir(),
		"note": "快照已存档：PNG 供人工/像素比对，JSON 里的指纹供 ui.diff 做结构化差异；" +
			"跨刷新 / HMR 的指纹不可比（ui.diff 会按 revision 提示）。",
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUISnapshot)), nil
}

// uiDiffRegion 像素差异区域（截图坐标系）。
type uiDiffRegion struct {
	X            int     `json:"x"`
	Y            int     `json:"y"`
	W            int     `json:"w"`
	H            int     `json:"h"`
	ChangedRatio float64 `json:"changedRatio"`
}

// uiDiff 对比两次快照：像素差异 + 指纹差异 + revision 提示，并输出 diff 图。
func (s *Server) uiDiff(args map[string]any) (*UIResult, error) {
	nameA := uiArgString(args, "a")
	nameB := uiArgString(args, "b")
	if nameA == "" || nameB == "" {
		return nil, fmt.Errorf("%w: 需要 a 与 b（两次 ui.snapshot 的名字）", ErrBadInput)
	}
	metaA, err := uiLoadSnapshot(nameA)
	if err != nil {
		return nil, err
	}
	metaB, err := uiLoadSnapshot(nameB)
	if err != nil {
		return nil, err
	}
	imgA, pathA, err := uiLoadSnapshotPNG(nameA)
	if err != nil {
		return nil, err
	}
	imgB, pathB, err := uiLoadSnapshotPNG(nameB)
	if err != nil {
		return nil, err
	}
	threshold := uiClampInt(args["threshold"], uiDiffDefThresh, 0, 255)

	boundsA, boundsB := imgA.Bounds(), imgB.Bounds()
	common := image.Rect(0, 0, minInt(boundsA.Dx(), boundsB.Dx()), minInt(boundsA.Dy(), boundsB.Dy()))
	if common.Empty() {
		return nil, fmt.Errorf("%w: 两张快照没有公共区域（%dx%d vs %dx%d），无法做像素差异",
			ErrBadInput, boundsA.Dx(), boundsA.Dy(), boundsB.Dx(), boundsB.Dy())
	}
	cropA := uiCropImage(imgA, common)
	cropB := uiCropImage(imgB, common)

	pd := uiPixelDiff(cropA, cropB, uiDiffTile, threshold)
	regions := uiMergeTiles(pd.grid, pd.tilesX, pd.tilesY, uiDiffTile)
	regionOut := make([]any, 0, len(regions))
	for i, r := range regions {
		if i >= uiDiffMaxRegions {
			regionOut = append(regionOut, map[string]any{
				"truncated":     true,
				"truncatedNote": fmt.Sprintf("还有 %d 个变化区域未列出（按面积排序，只保留前 %d 个）", len(regions)-uiDiffMaxRegions, uiDiffMaxRegions),
			})
			break
		}
		regionOut = append(regionOut, r)
	}

	diffImg := uiOverlayChanged(cropA, pd.grid, pd.tilesX, pd.tilesY, uiDiffTile, 110)
	diffBytes, err := uiEncodePNG(diffImg)
	if err != nil {
		return nil, err
	}
	diffPath := filepath.Join(uiSnapshotDir(), fmt.Sprintf("%s--%s.diff.png", nameA, nameB))
	if err := uiWriteFile(diffPath, diffBytes); err != nil {
		return nil, err
	}

	fpDiff := uiFingerprintDiff(metaA.Fingerprint, metaB.Fingerprint)

	revisionA := uiFingerprintKey(metaA.Revision)
	revisionB := uiFingerprintKey(metaB.Revision)
	reloaded := revisionA != revisionB
	data := map[string]any{
		"a": nameA, "b": nameB,
		"threshold":     threshold,
		"tile":          uiDiffTile,
		"changedRatio":  math.Round(pd.changedRatio*10000) / 10000,
		"changedPixels": pd.changedPixels,
		"totalPixels":   common.Dx() * common.Dy(),
		"changedTiles":  pd.changedTiles,
		"totalTiles":    pd.tilesX * pd.tilesY,
		"regions":       regionOut,
		"regionCount":   len(regions),
		"imageSize":     map[string]int{"w": common.Dx(), "h": common.Dy()},
		"pngA":          pathA, "pngB": pathB,
		"diffPngPath": diffPath,
		"fingerprint": fpDiff,
		"revisionA":   metaA.Revision, "revisionB": metaB.Revision,
		"tsA": metaA.TS, "tsB": metaB.TS,
	}
	if boundsA.Dx() != boundsB.Dx() || boundsA.Dy() != boundsB.Dy() {
		data["sizeMismatch"] = fmt.Sprintf("两张快照尺寸不同（A %dx%d，B %dx%d），已按最小公共区域 %dx%d 裁剪后比较",
			boundsA.Dx(), boundsA.Dy(), boundsB.Dx(), boundsB.Dy(), common.Dx(), common.Dy())
	}
	if reloaded {
		data["reloadWarning"] = fmt.Sprintf("两次快照之间发生过页面重载 / HMR（revision %s → %s）：DOM 与指纹差异可能不可比，"+
			"像素差异仍可参考（若窗口尺寸未变）。", revisionA, revisionB)
	}
	data["note"] = "图片是 diff 图：原图（A）上把发生变化的 16×16 块叠了半透明红；regions 是合并相邻变化块后的包围盒（最多 20 个）。"
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIDiff)).addImage("diff（红色=变化区域）", diffBytes), nil
}

// uiPixelDiffResult 像素差异结果（网格 + 统计）。
type uiPixelDiffResult struct {
	changedRatio  float64
	tilesX        int
	tilesY        int
	changedTiles  int
	changedPixels int
	grid          []bool
}

// uiPixelDiff 按 tile×tile 分块比较两张图：每块标记「是否有超过阈值的像素」，并统计整体变化比例。
func uiPixelDiff(a, b image.Image, tile, threshold int) uiPixelDiffResult {
	ba, bb := a.Bounds(), b.Bounds()
	w := minInt(ba.Dx(), bb.Dx())
	h := minInt(ba.Dy(), bb.Dy())
	if tile <= 0 {
		tile = uiDiffTile
	}
	tilesX := (w + tile - 1) / tile
	tilesY := (h + tile - 1) / tile
	res := uiPixelDiffResult{tilesX: tilesX, tilesY: tilesY, grid: make([]bool, tilesX*tilesY)}
	if w <= 0 || h <= 0 {
		return res
	}
	for y := 0; y < h; y++ {
		ty := y / tile
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := uiPixelAt(a, ba.Min.X+x, ba.Min.Y+y)
			r2, g2, b2, _ := uiPixelAt(b, bb.Min.X+x, bb.Min.Y+y)
			if uiChannelDiff(r1, r2) <= threshold && uiChannelDiff(g1, g2) <= threshold && uiChannelDiff(b1, b2) <= threshold {
				continue
			}
			res.changedPixels++
			idx := ty*tilesX + x/tile
			if !res.grid[idx] {
				res.grid[idx] = true
				res.changedTiles++
			}
		}
	}
	res.changedRatio = float64(res.changedPixels) / float64(w*h)
	return res
}

// uiChannelDiff 两个通道值的绝对差（避免 uint8 相减溢出）。
func uiChannelDiff(a, b uint8) int {
	if a > b {
		return int(a) - int(b)
	}
	return int(b) - int(a)
}

// uiPixelAt 取像素 RGB（对 RGBA / NRGBA 走快路径，其余走 At()）。
func uiPixelAt(img image.Image, x, y int) (uint8, uint8, uint8, uint8) {
	switch t := img.(type) {
	case *image.RGBA:
		i := t.PixOffset(x, y)
		return t.Pix[i], t.Pix[i+1], t.Pix[i+2], t.Pix[i+3]
	case *image.NRGBA:
		i := t.PixOffset(x, y)
		return t.Pix[i], t.Pix[i+1], t.Pix[i+2], t.Pix[i+3]
	default:
		c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
		return c.R, c.G, c.B, c.A
	}
}

// uiMergeTiles 把相邻的变化块合并成区域（8 连通 + 包围盒），按面积（含变化块数）从大到小排序。
func uiMergeTiles(grid []bool, tilesX, tilesY, tile int) []uiDiffRegion {
	if tilesX <= 0 || tilesY <= 0 || len(grid) != tilesX*tilesY {
		return nil
	}
	seen := make([]bool, len(grid))
	var regions []uiDiffRegion
	for idx := 0; idx < len(grid); idx++ {
		if !grid[idx] || seen[idx] {
			continue
		}
		// 广度优先扩展连通块
		queue := []int{idx}
		seen[idx] = true
		minTX, minTY := idx%tilesX, idx/tilesX
		maxTX, maxTY := minTX, minTY
		count := 0
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			count++
			cx, cy := cur%tilesX, cur/tilesX
			if cx < minTX {
				minTX = cx
			}
			if cy < minTY {
				minTY = cy
			}
			if cx > maxTX {
				maxTX = cx
			}
			if cy > maxTY {
				maxTY = cy
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := cx+dx, cy+dy
					if nx < 0 || ny < 0 || nx >= tilesX || ny >= tilesY {
						continue
					}
					ni := ny*tilesX + nx
					if grid[ni] && !seen[ni] {
						seen[ni] = true
						queue = append(queue, ni)
					}
				}
			}
		}
		x := minTX * tile
		y := minTY * tile
		w := (maxTX - minTX + 1) * tile
		h := (maxTY - minTY + 1) * tile
		area := w * h
		ratio := 1.0
		if area > 0 {
			ratio = float64(count) * float64(tile*tile) / float64(area)
		}
		regions = append(regions, uiDiffRegion{
			X: x, Y: y, W: w, H: h,
			ChangedRatio: math.Round(ratio*10000) / 10000,
		})
	}
	sort.SliceStable(regions, func(i, j int) bool {
		return regions[i].W*regions[i].H > regions[j].W*regions[j].H
	})
	return regions
}

// uiOverlayChanged 在 base 上把发生变化的块叠一层半透明红（diff 图）。
func uiOverlayChanged(base image.Image, grid []bool, tilesX, tilesY, tile int, alpha uint8) image.Image {
	b := base.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), base, b.Min, draw.Src)
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			if ty*tilesX+tx >= len(grid) || !grid[ty*tilesX+tx] {
				continue
			}
			x0, y0 := tx*tile, ty*tile
			x1, y1 := minInt(x0+tile, dst.Bounds().Dx()), minInt(y0+tile, dst.Bounds().Dy())
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := dst.RGBAAt(x, y)
					dst.SetRGBA(x, y, color.RGBA{
						R: uint8((int(c.R)*(255-int(alpha)) + 255*int(alpha)) / 255),
						G: uint8(int(c.G) * (255 - int(alpha)) / 255),
						B: uint8(int(c.B) * (255 - int(alpha)) / 255),
						A: 255,
					})
				}
			}
		}
	}
	return dst
}

// uiFingerprintFields 指纹里参与比对的字段。
var uiFingerprintFields = []string{"rect", "classes", "zIndex", "visible", "text", "styles"}

// uiFingerprintKey 把任意值压成可比较的稳定字符串（revision 可能是数字也可能是字符串）。
func uiFingerprintKey(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// uiFingerprintDiff 对比两次快照的指纹：added / removed / changed（只报真正变化的字段）。
func uiFingerprintDiff(a, b []map[string]any) map[string]any {
	index := func(items []map[string]any) map[string]map[string]any {
		out := make(map[string]map[string]any, len(items))
		for _, it := range items {
			path := uiArgString(it, "selectorPath")
			if path == "" {
				continue
			}
			out[path] = it
		}
		return out
	}
	am, bm := index(a), index(b)
	added, removed := []any{}, []any{}
	for path, it := range bm {
		if _, ok := am[path]; !ok {
			added = append(added, uiFingerprintBrief(it))
		}
	}
	for path, it := range am {
		if _, ok := bm[path]; !ok {
			removed = append(removed, uiFingerprintBrief(it))
		}
	}
	changed := []any{}
	for path, ia := range am {
		ib, ok := bm[path]
		if !ok {
			continue
		}
		fields := []any{}
		for _, f := range uiFingerprintFields {
			va, vb := ia[f], ib[f]
			if uiFingerprintKey(va) == uiFingerprintKey(vb) {
				continue
			}
			// styles 细化到**属性级**（M9）：只报真正变化的 CSS 属性，
			// 形状是 {name:"styles", styles:{属性:{from,to}}}；一个属性都没变就不出现这条。
			// 其余字段保持既有形状 {name, from, to}（不破坏既有返回字段）。
			if f == "styles" {
				props := uiStyleDiff(va, vb)
				if len(props) == 0 {
					continue
				}
				fields = append(fields, map[string]any{"name": f, "styles": props})
				continue
			}
			fields = append(fields, map[string]any{
				"name": f,
				"from": uiClipValue(va, 120, 20, 20, 2),
				"to":   uiClipValue(vb, 120, 20, 20, 2),
			})
		}
		if len(fields) > 0 {
			changed = append(changed, map[string]any{
				"selectorPath": path,
				"tag":          ib["tag"],
				"fields":       fields,
			})
		}
	}
	sortAnyByString(added, "selectorPath")
	sortAnyByString(removed, "selectorPath")
	sortAnyByString(changed, "selectorPath")
	out := map[string]any{
		"addedCount": len(added), "removedCount": len(removed), "changedCount": len(changed),
		"added": added, "removed": removed, "changed": changed,
		"note": "指纹只覆盖可见元素（最多 400 个）的 rect / classes / zIndex / visible / text / 关键样式；" +
			"changed[].fields[] 只报真正变化的字段，其中 styles 细化到**属性级**：{name:\"styles\", styles:{属性:{from,to}}}，" +
			"只列出值真的变了的 CSS 属性（没有属性变化就不出现该条目）；" +
			"selectorPath 是结构路径，两次快照之间若结构变化，同一元素会表现为「一个 removed + 一个 added」。",
	}
	if len(changed) > uiDiffMaxChanges {
		out["changed"] = changed[:uiDiffMaxChanges]
		out["truncated"] = true
		out["truncatedNote"] = fmt.Sprintf("变化元素过多（%d 个），只列出前 %d 个", len(changed), uiDiffMaxChanges)
	}
	return out
}

// uiStyleDiff 只列出**真正变化的样式属性**：{属性名: {from, to}}。
//
// 为什么细化到属性级：指纹里的 styles 子集有二十来个属性，若整份 from/to 丢回去，
// AI 还得自己做一遍 diff —— 而「只改了一个 --radius / 一个 color」是最常见的场景，
// 直接告诉它哪几个属性变了，既省 token 也不容易看漏（M9 修的最小粒度问题）。
//
// 约定：
//   - 两侧任意一侧出现过的属性都会参与比较（新增 / 删除的属性也会报，from/to 为 nil）；
//   - 值未变（按规范化 JSON 比较）的属性不出现；
//   - 值按既有规则裁剪（超长字符串截断、嵌套对象降级为形状描述），保证体积有界。
func uiStyleDiff(va, vb any) map[string]any {
	a, b := uiMap(va), uiMap(vb)
	seen := make(map[string]bool, len(a)+len(b))
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if uiFingerprintKey(a[k]) == uiFingerprintKey(b[k]) {
			continue
		}
		out[k] = map[string]any{
			"from": uiClipValue(a[k], 120, 4, 4, 2),
			"to":   uiClipValue(b[k], 120, 4, 4, 2),
		}
	}
	return out
}

// uiFingerprintBrief 指纹条目的精简形式（added / removed 只报关键字段）。
func uiFingerprintBrief(it map[string]any) map[string]any {
	return map[string]any{
		"selectorPath": it["selectorPath"],
		"tag":          it["tag"],
		"rect":         uiClipValue(it["rect"], 40, 4, 4, 3),
		"classes":      uiClipValue(it["classes"], 40, 6, 6, 3),
		"text":         uiClipValue(it["text"], 60, 1, 1, 3),
	}
}

// sortAnyByString 按字符串字段排序 []any 里的对象（顺序稳定，便于人读与比对）。
func sortAnyByString(items []any, key string) {
	sort.SliceStable(items, func(i, j int) bool {
		mi, mj := uiMap(items[i]), uiMap(items[j])
		return uiArgString(mi, key) < uiArgString(mj, key)
	})
}

// ---- assert ----

// uiAssert 断言界面状态：Go 侧判定（前端只提供元素事实），这样判定逻辑可单元测试。
func (s *Server) uiAssert(ctx context.Context, fe uiFrontend, args map[string]any) (*UIResult, error) {
	selector := uiArgString(args, "selector")
	if selector == "" {
		return nil, fmt.Errorf("%w: 缺少 selector（要断言的元素）", ErrBadInput)
	}
	props := []string{}
	for name := range uiMap(args["styles"]) {
		props = append(props, name)
	}
	sort.Strings(props)
	facts, err := uiFetch(ctx, fe, uiOpElementFacts, map[string]any{"selector": selector, "props": props})
	if err != nil {
		return nil, err
	}
	passed, checks := uiAssertChecks(facts, args)
	failed := 0
	for i := range checks {
		if !uiBool(checks[i], "passed") {
			failed++
		}
	}
	data := map[string]any{
		"selector":    selector,
		"passed":      passed,
		"checks":      checks,
		"failedCount": failed,
		"found":       uiBool(facts, "found"),
		"count":       facts["count"],
		"rect":        facts["rect"],
		"visible":     uiBool(facts, "found") && uiBool(facts, "visible"),
		"text":        uiClipString(uiArgString(facts, "text"), uiMaxStringRunes),
	}
	return uiResult(uiLimitData(data, uiMaxDataBytes, OpUIAssert)), nil
}

// uiAssertChecks 逐条判定断言，返回总体是否通过 + 每条的中文结果（期望 vs 实际）。
func uiAssertChecks(facts map[string]any, args map[string]any) (bool, []map[string]any) {
	checks := []map[string]any{}
	passed := true
	add := func(name string, ok bool, expected, actual any, reason string) {
		item := map[string]any{"name": name, "passed": ok, "expected": expected, "actual": actual}
		if !ok {
			passed = false
			if reason == "" {
				// 兜底：任何失败项都必须带中文原因，否则 AI 只能看到 passed=false
				reason = fmt.Sprintf("期望 %v，实际 %v", expected, actual)
			}
			item["reason"] = reason
		}
		checks = append(checks, item)
	}

	found := uiBool(facts, "found")
	visible := found && uiBool(facts, "visible")
	text := strings.TrimSpace(uiArgString(facts, "text"))
	rect, hasRect := uiRectFromAny(facts["rect"])

	if v, has := args["exists"]; has && v != nil {
		want := v == true
		reason := ""
		if !want && found {
			reason = fmt.Sprintf("未找到选择器 %q：元素不在 DOM 中", uiArgString(args, "selector"))
		} else if want && !found {
			reason = fmt.Sprintf("元素存在但期望不存在（选择器 %q）", uiArgString(args, "selector"))
		}
		add("exists", found == want, want, found, reason)
	}
	if v, has := args["visible"]; has && v != nil {
		want := v == true
		reason := ""
		if want && !found {
			reason = "元素不存在，因此不可能可见"
		} else if want && found && !visible {
			reason = "元素在 DOM 中但不可见（display:none / visibility:hidden / opacity:0 / 尺寸为 0）"
		}
		add("visible", visible == want, want, visible, reason)
	}
	if v, has := args["text"]; has && v != nil {
		want := strings.TrimSpace(fmt.Sprintf("%v", v))
		reason := ""
		if !found {
			reason = "元素不存在，无法比较文本"
		} else if text != want {
			reason = fmt.Sprintf("实际文本：%q", uiClipString(text, 200))
		}
		add("text", text == want, want, text, reason)
	}
	if v, has := args["textContains"]; has && v != nil {
		want := fmt.Sprintf("%v", v)
		reason := ""
		if !found {
			reason = "元素不存在，无法比较文本"
		} else if !strings.Contains(text, want) {
			reason = fmt.Sprintf("实际文本：%q", uiClipString(text, 200))
		}
		add("textContains", strings.Contains(text, want), want, uiClipString(text, 200), reason)
	}
	if v, has := args["rectWithin"]; has && v != nil {
		box, ok := uiRectFromAny(v)
		want := map[string]any{"x": box.X, "y": box.Y, "w": box.W, "h": box.H}
		reason := ""
		ok2 := false
		switch {
		case !found:
			reason = "元素不存在，无法比较矩形"
		case !hasRect:
			reason = "未取到元素矩形（元素可能没有布局盒）"
		case !ok:
			reason = "rectWithin 需要 w/h 为正的 {x,y,w,h}"
		default:
			inside := rect.X >= box.X-1 && rect.Y >= box.Y-1 &&
				rect.X+rect.W <= box.X+box.W+1 && rect.Y+rect.H <= box.Y+box.H+1
			ok2 = inside
			if !inside {
				reason = fmt.Sprintf("元素矩形 %+v 未落在 %+v 内", rect, box)
			}
		}
		add("rectWithin", ok2, want, map[string]any{"x": rect.X, "y": rect.Y, "w": rect.W, "h": rect.H}, reason)
	}
	if wantStyles := uiMap(args["styles"]); len(wantStyles) > 0 {
		got := uiMap(facts["styles"])
		names := make([]string, 0, len(wantStyles))
		for name := range wantStyles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			want := strings.TrimSpace(fmt.Sprintf("%v", wantStyles[name]))
			actual := strings.TrimSpace(uiArgString(got, name))
			reason := ""
			if !found {
				reason = "元素不存在，无法读取计算样式"
			} else if actual == "" {
				reason = fmt.Sprintf("未取到计算样式 %q（可能不是有效的 CSS 属性名）", name)
			} else if !strings.EqualFold(actual, want) {
				reason = fmt.Sprintf("(实际 %s: %s)", name, actual)
			}
			add("styles."+name, strings.EqualFold(actual, want) && actual != "", want, actual, reason)
		}
	}
	if len(checks) == 0 {
		add("no-checks", false, "至少一个断言条件", "无",
			"未给出任何断言条件：请至少传 exists / visible / text / textContains / rectWithin / styles 之一")
	}
	return passed, checks
}

// ---- 资源（ui://）----

// uiResourceURIs 本域提供的 MCP 资源。
var uiResourceURIs = []string{"ui://layout", "ui://view", "ui://console"}

// isUIResource 判断资源 uri 是否属于本域。
func isUIResource(uri string) bool {
	for _, u := range uiResourceURIs {
		if u == uri {
			return true
		}
	}
	return false
}

// mcpReadUIResource 读取 ui:// 资源（JSON 文本或纯文本）。
func (s *Server) mcpReadUIResource(ctx context.Context, uri string) (string, error) {
	fe := s.uiFrontend()
	switch uri {
	case "ui://layout":
		res, err := s.uiHandle(ctx, fe, OpUILayout, nil)
		if err != nil {
			return "", err
		}
		b, err := json.MarshalIndent(res.Data, "", "  ")
		return string(b), err
	case "ui://view":
		m, err := uiFetch(ctx, fe, uiOpView, nil)
		if err != nil {
			return "", err
		}
		b, err := json.MarshalIndent(uiLimitData(m, uiMaxDataBytes, OpUIRevision), "", "  ")
		return string(b), err
	case "ui://console":
		m, err := uiFetch(ctx, fe, OpUIConsole, nil)
		if err != nil {
			return "", err
		}
		entries := uiMapSlice(m["entries"])
		if len(entries) > 100 {
			entries = entries[len(entries)-100:]
		}
		var sb strings.Builder
		for _, e := range entries {
			parts := uiStringList(e["text"])
			sb.WriteString(fmt.Sprintf("[%s] %s\n", uiArgString(e, "level"), strings.Join(parts, " ")))
		}
		if sb.Len() == 0 {
			return "（控制台缓冲为空）", nil
		}
		return uiClipString(sb.String(), 20000), nil
	}
	return "", fmt.Errorf("未知资源: %s", uri)
}

// ---- 变化通知（resources/updated）----

// uiResourceUpdatedNotification 把 Hub 事件映射成 MCP 的 notifications/resources/updated。
//
// Hub 主题约定（由本域的观察器发布，见 uiStateWatcher）：
//
//	ui.view   → uri ui://view（当前视图 / 导航状态变了）
//	ui.layout → uri ui://layout（页面被重载 / HMR，布局认知整体失效）
func uiResourceUpdatedNotification(ev Event) (map[string]any, bool) {
	uri := ""
	switch ev.Topic {
	case "ui.view":
		uri = "ui://view"
	case "ui.layout":
		uri = "ui://layout"
	default:
		return nil, false
	}
	params := map[string]any{"uri": uri}
	if len(ev.Data) > 0 {
		var payload any
		if err := json.Unmarshal(ev.Data, &payload); err == nil {
			params["_meta"] = map[string]any{"ding-ssh/ui": payload}
		}
	}
	return map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated", "params": params}, true
}

// ---- 状态观察器 ----

// uiStateWatcher 轮询前端的 revision / view，变化时通过既有 Hub 广播主题事件，
// 让支持服务端推送的 MCP 客户端收到 notifications/resources/updated（见 mcpStream）。
//
// 为什么用轮询而不是让前端主动上报：前端 → Go 的通道目前只有 Wails 绑定（新增绑定会改动
// 自动生成文件与宿主 API），而 ui.revision 本身极轻（只读几个数字），1.5s 轮询完全够用。
//
// 自终止：连续 5 次调用失败（前端不可达 / 应用正在关闭）或前端没有实现 ui.revision
// （旧前端 / 测试替身）时自行退出，不会留下无意义的后台轮询。
type uiStateWatcher struct {
	srv      *Server
	fe       uiFrontend
	stopOnce sync.Once
	stop     chan struct{}
}

var (
	uiWatchersMu sync.Mutex
	uiWatchers   = map[*Server]*uiStateWatcher{}
)

// startUIWatcher 为某个 Server 启动观察器（幂等；每个 Server 最多一个）。
func startUIWatcher(s *Server) *uiStateWatcher {
	uiWatchersMu.Lock()
	if w, ok := uiWatchers[s]; ok {
		uiWatchersMu.Unlock()
		return w
	}
	w := &uiStateWatcher{srv: s, fe: s.uiFrontend(), stop: make(chan struct{})}
	uiWatchers[s] = w
	uiWatchersMu.Unlock()
	go w.run()
	return w
}

func (w *uiStateWatcher) close() {
	w.stopOnce.Do(func() { close(w.stop) })
	uiWatchersMu.Lock()
	delete(uiWatchers, w.srv)
	uiWatchersMu.Unlock()
}

func (w *uiStateWatcher) run() {
	if w.srv.opts.Hub == nil || w.fe == nil {
		w.close()
		return
	}
	timer := time.NewTimer(uiWatchInterval)
	defer timer.Stop()
	ticker := time.NewTicker(uiWatchInterval)
	defer ticker.Stop()
	<-timer.C
	failures := 0
	lastRevision, lastView := "", ""
	for {
		res, err := w.fe.CallUI(context.Background(), OpUIRevision, nil)
		if err != nil {
			failures++
			if failures >= 5 {
				w.close()
				return
			}
		} else {
			failures = 0
			m := uiMap(res)
			if m == nil {
				w.close()
				return
			}
			rev := uiFingerprintKey(m["revision"])
			if _, has := m["revision"]; !has {
				// 旧前端没实现 ui.revision：静默退出，避免无意义轮询（测试替身也走这条路）。
				w.close()
				return
			}
			view := uiArgString(uiMap(m["view"]), "view")
			if rev != lastRevision {
				if lastRevision != "" {
					w.publish("ui.layout", map[string]any{"revision": m["revision"], "view": view})
				}
				lastRevision = rev
			}
			if view != lastView {
				if lastView != "" {
					w.publish("ui.view", map[string]any{"view": view, "revision": m["revision"]})
				}
				lastView = view
			}
		}
		select {
		case <-w.stop:
			return
		case <-ticker.C:
		}
	}
}

func (w *uiStateWatcher) publish(topic string, payload map[string]any) {
	if w.srv.opts.Hub == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	w.srv.opts.Hub.Publish(Event{Topic: topic, Name: "ui:" + strings.TrimPrefix(topic, "ui."), Data: data})
}

// ---- HTTP 路由（server.go 里只留一行 registerUIRoutes 调用）----

// registerUIRoutes 注册 UI 域的全部 HTTP 路由，并启动 view / revision 观察器。
//
// 路由与 MCP 工具同名（/v1/ui/<tool>），能力校验 / 确认 / 审计复用 server.go 的
// read / write 包装（写类走 guarded，读类走 named —— 与既有路由完全一致的语义）。
func registerUIRoutes(mux *http.ServeMux, s *Server) {
	read := func(pattern, tool string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.named(tool, h))
	}
	write := func(pattern, tool string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.guarded(httpRoute{cap: CapUIWrite, tool: tool}, h))
	}

	// 读类（GET）：参数从 query 解析（buildArgs 会自动把数字/布尔转成对应类型）。
	read("GET /v1/ui/layout", OpUILayout, s.uiHTTP(OpUILayout))
	read("GET /v1/ui/query", OpUIQuery, s.uiHTTP(OpUIQuery))
	read("GET /v1/ui/styles", OpUIStyles, s.uiHTTP(OpUIStyles))
	read("GET /v1/ui/tokens", OpUITokens, s.uiHTTP(OpUITokens))
	read("GET /v1/ui/hit", OpUIHit, s.uiHTTP(OpUIHit))
	read("GET /v1/ui/tree", OpUITree, s.uiHTTP(OpUITree))
	read("GET /v1/ui/console", OpUIConsole, s.uiHTTP(OpUIConsole))
	read("GET /v1/ui/revision", OpUIRevision, s.uiHTTP(OpUIRevision))
	read("GET /v1/ui/screenshot", OpUIScreenshot, s.uiHTTP(OpUIScreenshot))
	read("GET /v1/ui/element-shot", OpUIElementShot, s.uiHTTP(OpUIElementShot))
	read("GET /v1/ui/store", OpUIStore, s.uiHTTP(OpUIStore))
	read("GET /v1/ui/diff", OpUIDiff, s.uiHTTP(OpUIDiff))
	// 需要结构化入参的读类（POST，但语义仍是读：不需要能力位）
	read("POST /v1/ui/assert", OpUIAssert, s.uiHTTP(OpUIAssert))
	read("POST /v1/ui/snapshot", OpUISnapshot, s.uiHTTP(OpUISnapshot))
	// 写类（POST）：需要 ui.write 能力位（可逆 / 高频，不需要 confirm token）
	write("POST /v1/ui/set-token", OpUISetToken, s.uiHTTP(OpUISetToken))
	write("POST /v1/ui/inject-css", OpUIInjectCSS, s.uiHTTP(OpUIInjectCSS))
	write("POST /v1/ui/store-patch", OpUIStorePatch, s.uiHTTP(OpUIStorePatch))
	write("POST /v1/ui/navigate", OpUINavigate, s.uiHTTP(OpUINavigate))
	write("POST /v1/ui/window", OpUIWindow, s.uiHTTP(OpUIWindow))
	write("POST /v1/ui/reload", OpUIReload, s.uiHTTP(OpUIReload))
	write("POST /v1/ui/click", OpUIClick, s.uiHTTP(OpUIClick))
	write("POST /v1/ui/type", OpUIType, s.uiHTTP(OpUIType))
	write("POST /v1/ui/key", OpUIKey, s.uiHTTP(OpUIKey))
	write("POST /v1/ui/hover", OpUIHover, s.uiHTTP(OpUIHover))
	write("POST /v1/ui/scroll", OpUIScroll, s.uiHTTP(OpUIScroll))

	startUIWatcher(s)
}

// uiHTTP 构造 UI 域某条路由的处理器：解析入参 → 走本域分发（含 Go 侧裁剪 / 差异 / 落盘）。
func (s *Server) uiHTTP(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args := map[string]any{}
		rawArgs, err := s.buildArgs(r, "")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "入参不是合法 JSON: " + err.Error()})
				return
			}
		}
		res, err := s.handleUIOp(r.Context(), op, args)
		if err != nil {
			writeJSON(w, statusFor(err), map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
