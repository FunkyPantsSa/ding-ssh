package apphost

import (
	"fmt"
	"strings"

	"ding-ssh/internal/sshx"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

func (a *App) GetHostTrust() ([]sshx.HostTrustRecord, error) { return sshx.ListHostTrust() }
func (a *App) DeleteHostTrust(line int, fingerprint string) error {
	return sshx.DeleteHostTrust(line, fingerprint)
}

// ConfirmHostTrust always requires a native user decision. API confirmation
// tokens and frontend booleans cannot approve a server key.
func (a *App) ConfirmHostTrust(host, publicKey string) (bool, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return false, err
	}
	if strings.ContainsAny(host, "\r\n") || len(host) > 1024 {
		return false, fmt.Errorf("无效主机地址")
	}
	answer, err := wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Type: wailsruntime.WarningDialog, Title: "首次连接 SSH 服务器",
		Message: fmt.Sprintf("主机：%s\n密钥类型：%s\n指纹：%s\n\n该服务器的身份尚未得到验证。请通过可信渠道核对主机指纹。", host, key.Type(), ssh.FingerprintSHA256(key)),
		Buttons: []string{"取消连接", "信任并连接"}, DefaultButton: "取消连接", CancelButton: "取消连接",
	})
	if err != nil || answer != "信任并连接" {
		return false, err
	}
	if err := sshx.SaveHostTrust(host, publicKey); err != nil {
		return false, err
	}
	return true, nil
}
