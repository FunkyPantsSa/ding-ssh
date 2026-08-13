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

// ProgressEvent SSH 连接过程进度事件。
type ProgressEvent struct {
	SessionID string `json:"sessionId"`
	Step      string `json:"step"`
}

// Settings 应用设置（持久化到 settings.json / SQLite settings 表）。
type Settings struct {
	LogEnabled            bool   `json:"logEnabled"`                      // 是否输出调试日志（默认关闭）
	CopyOnSelect          bool   `json:"copyOnSelect"`                    // 终端选中内容自动复制到剪贴板
	WebGLEnabled          bool   `json:"webGLEnabled"`                    // 优先使用 WebGL 渲染（失败自动降级）
	CompletionEnabled     bool   `json:"completionEnabled"`               // 智能命令补全
	CompletionNavHotkey   string `json:"completionNavHotkey"`             // 补全导航开关键键，如 Alt+ArrowDown
	CompletionPanelLimit  int    `json:"completionPanelLimit"`            // 补全面板最多展示条数，默认 8
	Theme                 Theme  `json:"theme"`                           // 终端主题
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
