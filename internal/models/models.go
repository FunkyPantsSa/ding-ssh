// Package models 定义 ding-ssh 前后端共享的数据结构。
package models

// ServerNode 服务器节点定义（与设计文档第 4 节保持一致）。
type ServerNode struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Group      string            `json:"group"` // 分组名，空为未分组
	Host       string            `json:"host"`
	Port       int               `json:"port"`
	User       string            `json:"user"`
	AuthType   string            `json:"authType"` // password | privateKey
	Password   string            `json:"password,omitempty"`
	KeyPath    string            `json:"keyPath,omitempty"`
	KeyContent string            `json:"keyContent,omitempty"` // 直接粘贴的私钥内容，优先于 KeyPath
	BgImage    string            `json:"bgImage"`
	BlurAmount int               `json:"blurAmount"`
	EnvVars    map[string]string `json:"envVars"`
}

// ConnectResult 连接结果，返回给前端用于建立会话。
type ConnectResult struct {
	SessionID string `json:"sessionId"`
	Server    string `json:"server"`
}

// ServerTestResult 服务器在线状态测试结果（TCP 延迟 + SSH 端口连通性）。
type ServerTestResult struct {
	NodeID    string `json:"nodeId"`          // 对应服务器节点 ID（批量测试时回填）
	LatencyMS int64  `json:"latencyMs"`       // TCP 握手延迟（毫秒），-1 表示连接失败
	Reachable bool   `json:"reachable"`       // SSH 端口是否畅通
	Error     string `json:"error,omitempty"` // 失败原因（超时 / 拒绝 / 不可达等）
}

// SessionStatus 会话状态。
type SessionStatus string

const (
	StatusConnecting SessionStatus = "connecting"
	StatusConnected  SessionStatus = "connected"
	StatusClosed     SessionStatus = "closed"
	StatusError      SessionStatus = "error"
	StatusDisconnected SessionStatus = "disconnected"
)

// SessionInfo 会话摘要信息。
type SessionInfo struct {
	SessionID  string        `json:"sessionId"`
	ServerName string        `json:"serverName"`
	Host       string        `json:"host"`
	User       string        `json:"user"`
	Status     SessionStatus `json:"status"`
	CreatedAt  int64         `json:"createdAt"`
}

// OutputEvent 终端输出事件（data 为 base64 编码的原始字节流）。
type OutputEvent struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

// StatusEvent 会话状态事件。
type StatusEvent struct {
	SessionID string        `json:"sessionId"`
	Status    SessionStatus `json:"status"`
	Message   string        `json:"message,omitempty"`
}

// 连接步骤标识，对应前端连接面板的五步展示。
const (
	ConnectStepDNS   = "dns"   // 1 DNS / 直连
	ConnectStepTCP   = "tcp"   // 2 TCP 握手
	ConnectStepAuth  = "auth"  // 3 SSH 鉴权
	ConnectStepPTY   = "pty"   // 4 分配 PTY
	ConnectStepReady = "ready" // 5 会话就绪
)

// ProgressEvent SSH 连接过程进度事件：每个事件代表一次步骤状态变更或一条步骤内详细日志，
// 前端按 step 聚合日志并支持展开排查卡住原因。
type ProgressEvent struct {
	SessionID string `json:"sessionId"`
	Step      string `json:"step"`              // dns | tcp | auth | pty | ready
	Status    string `json:"status"`            // running | done | error
	Log       string `json:"log,omitempty"`     // 追加到该步骤的一条详细日志
	Message   string `json:"message,omitempty"` // 步骤结束/失败时的摘要（错误时用于错误提示）
}

// Settings 应用设置（持久化到 settings.json / SQLite settings 表）。
type Settings struct {
	LogEnabled            bool   `json:"logEnabled"`                      // 是否输出调试日志（默认关闭）
	CopyOnSelect          bool   `json:"copyOnSelect"`                    // 终端选中内容自动复制到剪贴板
	WebGLEnabled          bool   `json:"webGLEnabled"`                    // 优先使用 WebGL 渲染（失败自动降级）
	CompletionEnabled     bool   `json:"completionEnabled"`               // 智能命令补全
	CompletionNavHotkey   string `json:"completionNavHotkey"`             // 补全导航开关键键，如 Alt+ArrowDown
	CompletionPanelLimit  int    `json:"completionPanelLimit"`            // 补全面板最多展示条数，默认 8
	SftpToTerminalSync    bool   `json:"sftpToTerminalSync"`              // SFTP 目录变化是否同步到终端（发 cd 命令），默认开启
	TerminalToSftpSync    bool   `json:"terminalToSftpSync"`              // 终端目录变化是否同步到 SFTP 面板，默认开启
	UIScale               int           `json:"uiScale"`                         // 界面缩放百分比，默认 100（80–150）
	Theme                 Theme         `json:"theme"`                          // 终端主题（含 ANSI 16 色）
	Appearance            UIAppearance  `json:"appearance"`                      // UI 外观（品牌色 + 明暗模式）
	Fonts                 Fonts         `json:"fonts"`                          // 字体设置
	AutoReconnect         bool          `json:"autoReconnect"`                   // 断开后自动重连（默认开启）
	KeepAliveEnabled      bool          `json:"keepAliveEnabled"`                // 发送心跳包防止终端超时（默认开启）
	LocalShell            string        `json:"localShell"`                      // 本机终端 Shell：darwin zsh|bash；windows powershell|cmd；linux default
}

// CommandHistory 命令历史记录（SQLite command_history 表）。
type CommandHistory struct {
	ID         int64  `json:"id"`
	ServerID   string `json:"serverId"`
	Command    string `json:"command"`
	ExecutedAt int64  `json:"executedAt"`
}

// CommandSuggestion 补全候选（含频次与来源，供前端排序展示）。
type CommandSuggestion struct {
	Command string `json:"command"`
	Count   int    `json:"count"`
	Source  string `json:"source"` // history | dict | screen
}

// Theme 终端主题设置。
type Theme struct {
	Background string `json:"background"` // 终端背景色（hex）
	Foreground string `json:"foreground"` // 文字颜色（hex）
	Cursor     string `json:"cursor"`     // 光标颜色（hex）
	Selection  string `json:"selection"`  // 选中背景色（hex，可带透明度）
	BgImage    string `json:"bgImage"`    // 背景图路径，空为不启用
	BlurAmount int    `json:"blurAmount"` // 背景图模糊强度(px)
	TextShadow bool   `json:"textShadow"` // 是否启用文字阴影
	ShadowBlur int    `json:"shadowBlur"` // 文字阴影模糊强度(px)

	// ANSI 16 色：终端内程序输出（ls / vim / 提示符等）的颜色映射。
	Black   string `json:"black"`
	Red     string `json:"red"`
	Green   string `json:"green"`
	Yellow  string `json:"yellow"`
	Blue    string `json:"blue"`
	Magenta string `json:"magenta"`
	Cyan    string `json:"cyan"`
	White   string `json:"white"`
	// 亮色变体（ANSI 90–97）。
	BrightBlack   string `json:"brightBlack"`
	BrightRed     string `json:"brightRed"`
	BrightGreen   string `json:"brightGreen"`
	BrightYellow  string `json:"brightYellow"`
	BrightBlue    string `json:"brightBlue"`
	BrightMagenta string `json:"brightMagenta"`
	BrightCyan    string `json:"brightCyan"`
	BrightWhite   string `json:"brightWhite"`
}

// UIAppearance UI 外观：品牌色与明暗模式。
type UIAppearance struct {
	Mode      string `json:"mode"`      // preset | custom
	PresetID  string `json:"presetId"`  // 预设主题 ID（preset 模式）
	BaseTone  string `json:"baseTone"`  // auto | light | dark
	Primary   string `json:"primary"`   // custom：主色（hex）
	Secondary string `json:"secondary"` // custom：辅色（hex）
	UiText    string `json:"uiText"`    // custom：界面主文字色（hex）
}

// Fonts 字体设置。
type Fonts struct {
	UiFont           string `json:"uiFont"`           // UI 字体名（如 Sora / system）
	TerminalFont     string `json:"terminalFont"`     // 终端等宽字体名
	TerminalFontSize int    `json:"terminalFontSize"` // 终端字号（默认 13）
}

// DefaultAppearance 返回默认外观（信号青绿预设 + 跟随系统明暗）。
func DefaultAppearance() UIAppearance {
	return UIAppearance{
		Mode:      "preset",
		PresetID:  "signal",
		BaseTone:  "auto",
		Primary:   "#3ec4b4",
		Secondary: "#c97a4a",
		UiText:    "#eef1f5",
	}
}

// DefaultFonts 返回默认字体设置。
func DefaultFonts() Fonts {
	return Fonts{
		UiFont:           "Sora",
		TerminalFont:     "IBM Plex Mono",
		TerminalFontSize: 13,
	}
}

// DefaultTheme 返回默认终端主题。
func DefaultTheme() Theme {
	return Theme{
		Background: "#0c1016",
		Foreground: "#d4dae3",
		Cursor:     "#3ec4b4",
		Selection:  "rgba(42, 168, 154, 0.28)",
		BgImage:    "",
		BlurAmount: 12,
		TextShadow: false,
		ShadowBlur: 3,

		Black:         "#0c1016",
		Red:           "#d45a5a",
		Green:         "#4caf7a",
		Yellow:        "#d4a04a",
		Blue:          "#5c8fe6",
		Magenta:       "#b26cd9",
		Cyan:          "#3ec4b4",
		White:         "#d4dae3",
		BrightBlack:   "#6b7684",
		BrightRed:     "#f08080",
		BrightGreen:   "#7ad8a3",
		BrightYellow:  "#e8c070",
		BrightBlue:    "#8fb0f2",
		BrightMagenta: "#d39cf0",
		BrightCyan:    "#6dd9cb",
		BrightWhite:   "#ffffff",
	}
}

// FillThemeAnsi 为缺失的 ANSI 16 色字段补齐默认值，保留用户已有的自定义值。
func FillThemeAnsi(t *Theme) {
	d := DefaultTheme()
	if t.Black == "" {
		t.Black = d.Black
	}
	if t.Red == "" {
		t.Red = d.Red
	}
	if t.Green == "" {
		t.Green = d.Green
	}
	if t.Yellow == "" {
		t.Yellow = d.Yellow
	}
	if t.Blue == "" {
		t.Blue = d.Blue
	}
	if t.Magenta == "" {
		t.Magenta = d.Magenta
	}
	if t.Cyan == "" {
		t.Cyan = d.Cyan
	}
	if t.White == "" {
		t.White = d.White
	}
	if t.BrightBlack == "" {
		t.BrightBlack = d.BrightBlack
	}
	if t.BrightRed == "" {
		t.BrightRed = d.BrightRed
	}
	if t.BrightGreen == "" {
		t.BrightGreen = d.BrightGreen
	}
	if t.BrightYellow == "" {
		t.BrightYellow = d.BrightYellow
	}
	if t.BrightBlue == "" {
		t.BrightBlue = d.BrightBlue
	}
	if t.BrightMagenta == "" {
		t.BrightMagenta = d.BrightMagenta
	}
	if t.BrightCyan == "" {
		t.BrightCyan = d.BrightCyan
	}
	if t.BrightWhite == "" {
		t.BrightWhite = d.BrightWhite
	}
}

// Credential 保存的常用凭证（用户名 + 密码）。
type Credential struct {
	ID         string `json:"id"`
	Name       string `json:"name"` // 凭证名称，如「生产 root」
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	AuthType   string `json:"authType"` // password | privateKey
	KeyPath    string `json:"keyPath,omitempty"`
	KeyContent string `json:"keyContent,omitempty"`
}

// TunnelInfo SSH 隧道摘要信息。
type TunnelInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	Mode       string `json:"mode"`       // local | remote | dynamic
	LocalPort  int    `json:"localPort"`  // 本地监听端口（local/dynamic）或本地目标端口（remote）
	RemoteHost string `json:"remoteHost"` // 远程目标主机（local）或远程监听地址（remote）
	RemotePort int    `json:"remotePort"` // 远程目标/监听端口（dynamic 可为 0）
	Status     string `json:"status"`     // running | stopped | error
	Message    string `json:"message,omitempty"`
	StartedAt  int64  `json:"startedAt"`
}

// TunnelStatusEvent 隧道状态变更事件（事件名 tunnel:status）。
type TunnelStatusEvent struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // running | stopped | error
	Message string `json:"message,omitempty"`
}

// SysInfoSnapshot 系统信息快照（SysInfo Dashboard / 状态栏）。
type SysInfoSnapshot struct {
	SessionID   string      `json:"sessionId"`
	CPUUsage    float64     `json:"cpuUsage"`
	MemUsedMB   uint64      `json:"memUsedMb"`
	MemTotalMB  uint64      `json:"memTotalMb"`
	DiskUsage   []DiskInfo  `json:"diskUsage"`
	NetIfaces   []NetIface  `json:"netIfaces"`
	Uptime      string      `json:"uptime"`
	CollectedAt int64       `json:"collectedAt"`
	Error       string      `json:"error,omitempty"`
}

// DiskInfo 磁盘分区用量。
type DiskInfo struct {
	MountPoint string  `json:"mountPoint"`
	TotalGB    uint64  `json:"totalGb"`
	UsedGB     uint64  `json:"usedGb"`
	UsagePct   float64 `json:"usagePct"`
}

// NetIface 网卡流量（速率由相邻两次采样差分得到；可附带 IPv4）。
type NetIface struct {
	Name   string  `json:"name"`
	IP     string  `json:"ip,omitempty"`
	RxMbps float64 `json:"rxMbps"`
	TxMbps float64 `json:"txMbps"`
}

// SecurityStatus 敏感字段加密 / 主密码状态。
type SecurityStatus struct {
	Unlocked              bool `json:"unlocked"`
	MasterPasswordEnabled bool `json:"masterPasswordEnabled"`
	KeyringAvailable      bool `json:"keyringAvailable"`
	NeedsUnlock           bool `json:"needsUnlock"`
}

// ImportConfigResult 配置导入结果。
type ImportConfigResult struct {
	Servers     int `json:"servers"`
	Credentials int `json:"credentials"`
	Groups      int `json:"groups"`
}

// SFTPEntry 远程目录条目。
type SFTPEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

// SFTPTransferEvent SFTP 传输进度事件。
type SFTPTransferEvent struct {
	SessionID   string `json:"sessionId"`
	Direction   string `json:"direction"` // upload | download
	Name        string `json:"name"`
	Transferred int64  `json:"transferred"`
	Total       int64  `json:"total"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// DirSyncEvent 终端与 SFTP 目录同步事件。
type DirSyncEvent struct {
	SessionID   string `json:"sessionId"`
	CurrentPath string `json:"currentPath"`
	Source      string `json:"source"` // "terminal" or "sftp"
}
