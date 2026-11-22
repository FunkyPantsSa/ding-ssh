package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var nativeApprovalMu sync.Mutex

// The confirmation is a native dialog, outside the scriptable application DOM.
// A missing app context fails closed, which also makes headless testing safe.
func (c *debugController) approveDebugOperation(ctx context.Context, req debugsrv.ApprovalRequest) (debugsrv.ApprovalDecision, error) {
	if c.app == nil || (c.app.ctx == nil && c.app.debugDialog == nil) {
		return debugsrv.ApprovalDecision{}, errors.New("应用尚未就绪，无法显示可信审批窗口")
	}
	nativeApprovalMu.Lock()
	defer nativeApprovalMu.Unlock()
	if ctx.Err() != nil {
		return debugsrv.ApprovalDecision{}, ctx.Err()
	}
	message := fmt.Sprintf("客户端：%s\n操作：%s\n\n%s\n\n仅批准这次操作，或在 10 分钟内批准相同参数的同一操作。", req.ClientID, req.Tool, req.Summary)
	if len(message) > 12000 {
		return debugsrv.ApprovalDecision{}, errors.New("操作摘要过长，请缩小变更范围")
	}
	choice, err := c.app.showDebugDialog(wailsruntime.MessageDialogOptions{Type: wailsruntime.QuestionDialog, Title: "MCP 操作授权", Message: message, Buttons: []string{"拒绝", "允许一次", "同一操作允许 10 分钟"}, DefaultButton: "拒绝", CancelButton: "拒绝"})
	if err != nil {
		return debugsrv.ApprovalDecision{}, err
	}
	if ctx.Err() != nil {
		return debugsrv.ApprovalDecision{}, ctx.Err()
	}
	return debugsrv.ApprovalDecision{Approved: choice == "允许一次" || choice == "同一操作允许 10 分钟", AllowFor: func() time.Duration {
		if choice == "同一操作允许 10 分钟" {
			return 10 * time.Minute
		}
		return 0
	}()}, nil
}

// Client grants can only be created through the native host UI. This binding is
// deliberately absent from the MCP tool catalog and always asks the user.
func (a *App) CreateDebugClient(policyJSON string) (map[string]any, error) {
	if a.debug == nil || a.debug.srv == nil {
		return nil, errors.New("调试服务未启动")
	}
	var p debugsrv.ClientPolicy
	if err := json.Unmarshal([]byte(policyJSON), &p); err != nil {
		return nil, errors.New("客户端策略不是合法 JSON")
	}
	if a.ctx == nil && a.debugDialog == nil {
		return nil, errors.New("无法显示可信授权窗口")
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	nativeApprovalMu.Lock()
	choice, err := a.showDebugDialog(wailsruntime.MessageDialogOptions{Type: wailsruntime.QuestionDialog, Title: "创建 MCP 客户端授权", Message: "请核对客户端、目标范围、权限和到期时间：\n" + string(data), Buttons: []string{"拒绝", "创建授权"}, DefaultButton: "拒绝", CancelButton: "拒绝"})
	nativeApprovalMu.Unlock()
	if err != nil {
		return nil, err
	}
	if choice != "创建授权" {
		return nil, errors.New("用户拒绝授权")
	}
	return a.debug.srv.IssueClient(p)
}
func (a *App) ListDebugClients() []debugsrv.ClientPolicy {
	if a.debug == nil || a.debug.srv == nil {
		return []debugsrv.ClientPolicy{}
	}
	return a.debug.srv.ListClients()
}
func (a *App) RevokeDebugClient(id string) {
	if a.debug != nil && a.debug.srv != nil {
		a.debug.srv.RevokeClient(id)
		if a.audits != nil {
			a.audits.Append(debugsrv.AuditRecord{Source: "ui", Tool: "authorization.revoke", ClientID: id, OK: true})
		}
	}
}

func (a *App) configureDebugAudit(d models.DebugSettings) {
	if a.audits == nil {
		return
	}
	path := ""
	if d.AuditPersistent && a.dataDir != "" {
		path = filepath.Join(a.dataDir, "mcp-audit.jsonl")
	}
	if err := a.audits.ConfigurePersistence(path); err != nil {
		logx.Errorf("[debugsrv/audit] 无法配置持久审计: err=%v", err)
	}
}

func debugCapabilityValue(d models.DebugSettings, cap debugsrv.Capability) (bool, bool) {
	switch cap {
	case debugsrv.CapTerminalRead:
		return d.CapTerminalRead, true
	case debugsrv.CapRemoteFSRead:
		return d.CapRemoteFSRead, true
	case debugsrv.CapUIInspect:
		return d.CapUIInspect, true
	case debugsrv.CapConnect:
		return d.CapConnect, true
	case debugsrv.CapDisconnect:
		return d.CapDisconnect, true
	case debugsrv.CapTransferCancel:
		return d.CapTransferCancel, true
	case debugsrv.CapLocalExec:
		return d.CapLocalExec, true
	case debugsrv.CapCredentialUse:
		return d.CapCredentialUse, true
	case debugsrv.CapTrustedAutomation:
		return d.TrustedAutomation, true
	}
	return false, false
}
func setDebugCapability(d *models.DebugSettings, cap debugsrv.Capability, value bool) bool {
	switch cap {
	case debugsrv.CapTerminalRead:
		d.CapTerminalRead = value
	case debugsrv.CapRemoteFSRead:
		d.CapRemoteFSRead = value
	case debugsrv.CapUIInspect:
		d.CapUIInspect = value
	case debugsrv.CapConnect:
		d.CapConnect = value
	case debugsrv.CapDisconnect:
		d.CapDisconnect = value
	case debugsrv.CapTransferCancel:
		d.CapTransferCancel = value
	case debugsrv.CapLocalExec:
		d.CapLocalExec = value
	case debugsrv.CapCredentialUse:
		d.CapCredentialUse = value
	case debugsrv.CapTrustedAutomation:
		d.TrustedAutomation = value
	default:
		return false
	}
	return true
}

// Debug changes are protected even if an application service is invoked through
// a scriptable WebView binding. Arbitrary JS remains an explicit trust boundary.
func (a *App) approveDebugSettingsChange(before, after models.DebugSettings) error {
	x, _ := json.Marshal(before)
	y, _ := json.Marshal(after)
	if string(x) == string(y) {
		return nil
	}
	// Disabling the service/capabilities is always allowed. Any broader change
	// requires a native decision, independent of DOM click synthesis.
	var old, new map[string]any
	_ = json.Unmarshal(x, &old)
	_ = json.Unmarshal(y, &new)
	expansion := false
	for key, value := range new {
		if reflect.DeepEqual(value, old[key]) {
			continue
		}
		if b, ok := value.(bool); ok {
			if b {
				expansion = true
			}
		} else if key == "sftpWriteAllowlist" {
			for _, root := range after.SFTPWriteAllowlist {
				norm, err := debugsrv.NormalizeRemotePath(root)
				if err != nil || !debugsrv.SFTPPathAllowed(before.SFTPWriteAllowlist, norm) {
					expansion = true
				}
			}
		} else if key == "port" {
			expansion = true
		}
	}
	if !expansion {
		return nil
	}
	if a.ctx == nil && a.debugDialog == nil {
		return errors.New("权限或监听变更需要独立的用户批准")
	}
	nativeApprovalMu.Lock()
	defer nativeApprovalMu.Unlock()
	choice, err := a.showDebugDialog(wailsruntime.MessageDialogOptions{Type: wailsruntime.QuestionDialog, Title: "更改 MCP 授权", Message: "本次修改涉及调试权限、访问范围或监听端口。是否批准？\n\n" + string(y), Buttons: []string{"拒绝", "批准"}, DefaultButton: "拒绝", CancelButton: "拒绝"})
	if err != nil {
		return err
	}
	if choice != "批准" {
		return errors.New("用户拒绝权限变更")
	}
	return nil
}

func (a *App) showDebugDialog(opts wailsruntime.MessageDialogOptions) (string, error) {
	if a.debugDialog != nil {
		return a.debugDialog(opts)
	}
	if a.ctx == nil {
		return "", errors.New("可信用户审批不可用")
	}
	return wailsruntime.MessageDialog(a.ctx, opts)
}

func (a *App) ApplyDebugPreset(mode string) error {
	st, err := a.settings.Get()
	if err != nil {
		return err
	}
	old := st.Debug
	d := models.DefaultDebugSettings()
	d.Enabled = old.Enabled
	d.Port = old.Port
	d.BindLAN = old.BindLAN
	d.SFTPWriteAllowlist = old.SFTPWriteAllowlist
	switch mode {
	case "observe":
		d.CapTerminalRead = true
		d.CapRemoteFSRead = true
		d.CapUIInspect = true
	case "operate":
		d.CapTerminalRead = true
		d.CapRemoteFSRead = true
		d.CapConnect = true
		d.CapDisconnect = true
		d.CapCredentialUse = true
		d.CapTerminalInput = true
		d.CapTransferCancel = true
		d.CapConfigWrite = true
		d.CapRemoteFSWrite = true
	case "developer":
		d = old
		d.TrustedAutomation = true
		d.AllowEval = true
		d.CapUIWrite = true
		d.CapUIInspect = true
		d.CapTerminalRead = true
	default:
		return errors.New("预设必须为 observe / operate / developer")
	}
	st.Debug = d
	return a.SaveSettings(st)
}
