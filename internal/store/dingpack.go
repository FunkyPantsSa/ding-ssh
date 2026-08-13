package store

import (
	"encoding/json"
	"fmt"
	"os"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/models"
)

const dingpackMagic = "DINGPACK1"

// DingpackPayload .dingpack 明文载荷。
type DingpackPayload struct {
	Magic       string              `json:"magic"`
	Version     int                 `json:"version"`
	Servers     []models.ServerNode `json:"servers"`
	Credentials []models.Credential `json:"credentials"`
	Groups      []string            `json:"groups"`
}

// BuildDingpack 序列化并加密配置包。
func BuildDingpack(passphrase string, servers []models.ServerNode, creds []models.Credential, groups []string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("导出密码不能为空")
	}
	if len(servers) > 500 {
		return nil, fmt.Errorf("服务器数量超过上限 500")
	}
	payload := DingpackPayload{
		Magic:       dingpackMagic,
		Version:     1,
		Servers:     servers,
		Credentials: creds,
		Groups:      groups,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return cryptox.SealPayload(passphrase, raw)
}

// ParseDingpack 解密并解析配置包。
func ParseDingpack(passphrase string, blob []byte) (*DingpackPayload, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("导入密码不能为空")
	}
	raw, err := cryptox.OpenPayload(passphrase, blob)
	if err != nil {
		return nil, err
	}
	var payload DingpackPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("配置包损坏: %w", err)
	}
	if payload.Magic != dingpackMagic {
		return nil, fmt.Errorf("不是有效的 .dingpack 文件")
	}
	return &payload, nil
}

// WriteDingpackFile 写入加密配置包到文件。
func WriteDingpackFile(path, passphrase string, servers []models.ServerNode, creds []models.Credential, groups []string) error {
	blob, err := BuildDingpack(passphrase, servers, creds, groups)
	if err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o600)
}

// ReadDingpackFile 从文件读取并解密配置包。
func ReadDingpackFile(path, passphrase string) (*DingpackPayload, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDingpack(passphrase, blob)
}
