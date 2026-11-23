package store

import (
	"ding-ssh/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/logx"
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
	logx.Debugf("[store] 构建配置包: servers=%d credentials=%d groups=%d", len(servers), len(creds), len(groups))
	if passphrase == "" {
		logx.Warnf("[store] 构建配置包被拒绝: 导出密码为空")
		return nil, fmt.Errorf("%s", i18n.T("导出密码不能为空"))
	}
	if len(servers) > 500 {
		logx.Warnf("[store] 构建配置包被拒绝: servers=%d 超过上限 500", len(servers))
		return nil, fmt.Errorf("%s", i18n.T("服务器数量超过上限 500"))
	}
	start := time.Now()
	payload := DingpackPayload{
		Magic:       dingpackMagic,
		Version:     1,
		Servers:     servers,
		Credentials: creds,
		Groups:      groups,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		logx.Errorf("[store] 序列化配置包载荷失败: servers=%d err=%v", len(servers), err)
		return nil, err
	}
	blob, err := cryptox.SealPayload(passphrase, raw)
	if err != nil {
		logx.Errorf("[store] 加密配置包失败: payloadBytes=%d err=%v", len(raw), err)
		return nil, err
	}
	logx.Infof("[store] 配置包构建完成: servers=%d credentials=%d groups=%d payloadBytes=%d blobBytes=%d elapsed=%s",
		len(servers), len(creds), len(groups), len(raw), len(blob), time.Since(start))
	return blob, nil
}

// ParseDingpack 解密并解析配置包。
func ParseDingpack(passphrase string, blob []byte) (*DingpackPayload, error) {
	start := time.Now()
	logx.Debugf("[store] 解析配置包: bytes=%d", len(blob))
	if passphrase == "" {
		logx.Warnf("[store] 解析配置包被拒绝: 导入密码为空")
		return nil, fmt.Errorf("%s", i18n.T("导入密码不能为空"))
	}
	raw, err := cryptox.OpenPayload(passphrase, blob)
	if err != nil {
		if errors.Is(err, cryptox.ErrBadPassword) {
			logx.Warnf("[store] 配置包解密失败，密码可能错误: bytes=%d", len(blob))
		} else {
			logx.Errorf("[store] 配置包解密失败: bytes=%d err=%v", len(blob), err)
		}
		return nil, err
	}
	var payload DingpackPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		logx.Errorf("[store] 配置包内容解析失败（文件损坏）: payloadBytes=%d err=%v", len(raw), err)
		return nil, fmt.Errorf(i18n.T("配置包损坏: %w"), err)
	}
	if payload.Magic != dingpackMagic || payload.Version != 1 {
		logx.Warnf("[store] 配置包格式不匹配: magic=%q", payload.Magic)
		return nil, fmt.Errorf("%s", i18n.T("不是有效的 .dingpack 文件"))
	}
	if len(payload.Servers) > 500 {
		return nil, fmt.Errorf("%s", i18n.T("服务器数量超过上限 500"))
	}
	logx.Infof("[store] 配置包解析完成: version=%d servers=%d credentials=%d groups=%d payloadBytes=%d elapsed=%s",
		payload.Version, len(payload.Servers), len(payload.Credentials), len(payload.Groups), len(raw), time.Since(start))
	return &payload, nil
}

// WriteDingpackFile 写入加密配置包到文件。
func WriteDingpackFile(path, passphrase string, servers []models.ServerNode, creds []models.Credential, groups []string) error {
	start := time.Now()
	logx.Debugf("[store] 写入配置包文件: path=%q", path)
	blob, err := BuildDingpack(passphrase, servers, creds, groups)
	if err != nil {
		// BuildDingpack 已记录具体 ERROR，这里只补文件上下文，避免同一错误记两次 ERROR。
		logx.Debugf("[store] 写入配置包文件中止（构建阶段失败）: path=%q err=%v", path, err)
		return err
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		logx.Errorf("[store] 写入配置包文件失败: path=%q bytes=%d err=%v", path, len(blob), err)
		return err
	}
	logx.Infof("[store] 配置包文件已写出: path=%q bytes=%d servers=%d elapsed=%s",
		path, len(blob), len(servers), time.Since(start))
	return nil
}

// ReadDingpackFile 从文件读取并解密配置包。
func ReadDingpackFile(path, passphrase string) (*DingpackPayload, error) {
	start := time.Now()
	logx.Debugf("[store] 读取配置包文件: path=%q", path)
	blob, err := os.ReadFile(path)
	if err != nil {
		logx.Errorf("[store] 读取配置包文件失败: path=%q err=%v", path, err)
		return nil, err
	}
	payload, err := ParseDingpack(passphrase, blob)
	if err != nil {
		// ParseDingpack 已记录具体失败原因，这里只补文件上下文，避免重复 ERROR。
		logx.Debugf("[store] 读取配置包文件中止（解析阶段失败）: path=%q bytes=%d err=%v", path, len(blob), err)
		return nil, err
	}
	logx.Debugf("[store] 配置包文件读取完成: path=%q bytes=%d elapsed=%s", path, len(blob), time.Since(start))
	return payload, nil
}
