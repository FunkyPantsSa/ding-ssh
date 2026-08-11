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

// Settings 应用设置（持久化到 settings.json）。
type Settings struct {
	LogEnabled   bool  `json:"logEnabled"`   // 是否输出调试日志（默认关闭）
	CopyOnSelect bool  `json:"copyOnSelect"` // 终端选中内容自动复制到剪贴板
	Theme        Theme `json:"theme"`        // 终端主题
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
		Background: "#0b1120",
		Foreground: "#dbe4f0",
		Cursor:     "#38bdf8",
		Selection:  "rgba(56, 189, 248, 0.25)",
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

// TunnelInfo SSH 隧道摘要信息（本地端口转发）。
type TunnelInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	LocalPort  int    `json:"localPort"`  // 本地监听端口
	RemoteHost string `json:"remoteHost"` // 远程目标主机
	RemotePort int    `json:"remotePort"` // 远程目标端口
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
