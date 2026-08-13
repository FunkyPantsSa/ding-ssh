package cryptox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "ding-ssh"
	keyringUser    = "master-key"
	metaFileName   = "security.json"
)

// Meta 持久化的安全元数据（不含主密钥本身）。
type Meta struct {
	MasterPasswordEnabled bool   `json:"masterPasswordEnabled"`
	SaltB64               string `json:"saltB64,omitempty"` // Argon2 盐（主密码模式）
	VerifierB64           string `json:"verifierB64,omitempty"` // 用主密钥加密的固定校验串
	MigratedAt            int64  `json:"migratedAt,omitempty"`
}

// Status 对外暴露的安全状态。
type Status struct {
	Unlocked              bool `json:"unlocked"`
	MasterPasswordEnabled bool `json:"masterPasswordEnabled"`
	KeyringAvailable      bool `json:"keyringAvailable"`
	NeedsUnlock           bool `json:"needsUnlock"`
}

// Vault 管理主密钥生命周期与字段加解密。
type Vault struct {
	mu     sync.RWMutex
	key    []byte
	meta   Meta
	dir    string
	locked bool // true 表示需要主密码解锁
}

// OpenVault 加载安全元数据并尝试自动解锁（Keyring 模式）。
func OpenVault(configDir string) (*Vault, error) {
	if configDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			configDir = "."
		} else {
			configDir = filepath.Join(dir, "ding-ssh")
		}
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	v := &Vault{dir: configDir}
	if err := v.loadMeta(); err != nil {
		return nil, err
	}
	if v.meta.MasterPasswordEnabled {
		v.locked = true
		return v, nil
	}
	// 默认模式：从 Keyring 取主密钥，失败则生成并写入。
	key, err := v.loadOrCreateKeyringKey()
	if err != nil {
		// Keyring 不可用时回退到本地受保护文件（仍优于明文）。
		key, err = v.loadOrCreateFileKey()
		if err != nil {
			return nil, err
		}
	}
	v.key = key
	v.locked = false
	return v, nil
}

func (v *Vault) metaPath() string {
	return filepath.Join(v.dir, metaFileName)
}

func (v *Vault) fileKeyPath() string {
	return filepath.Join(v.dir, ".master.key")
}

func (v *Vault) loadMeta() error {
	data, err := os.ReadFile(v.metaPath())
	if errors.Is(err, os.ErrNotExist) {
		v.meta = Meta{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &v.meta)
}

func (v *Vault) saveMeta() error {
	data, err := json.MarshalIndent(v.meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := v.metaPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.metaPath())
}

func (v *Vault) loadOrCreateKeyringKey() ([]byte, error) {
	secret, err := keyring.Get(keyringService, keyringUser)
	if err == nil && secret != "" {
		key, err := base64.StdEncoding.DecodeString(secret)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("Keyring 中的主密钥无效")
		}
		return key, nil
	}
	key, err := NewMasterKey()
	if err != nil {
		return nil, err
	}
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, nil
}

func (v *Vault) loadOrCreateFileKey() ([]byte, error) {
	path := v.fileKeyPath()
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(string(data))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	key, err := NewMasterKey()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Status 返回当前安全状态。
func (v *Vault) Status() Status {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keyringOK := true
	if _, err := keyring.Get(keyringService, keyringUser); err != nil && !v.meta.MasterPasswordEnabled {
		// 探测：写入测试可能过重，仅标记可用性近似值
		keyringOK = errors.Is(err, keyring.ErrNotFound) || v.key != nil
	}
	return Status{
		Unlocked:              !v.locked && v.key != nil,
		MasterPasswordEnabled: v.meta.MasterPasswordEnabled,
		KeyringAvailable:      keyringOK,
		NeedsUnlock:           v.locked,
	}
}

// Unlock 使用主密码解锁。
func (v *Vault) Unlock(password string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.meta.MasterPasswordEnabled {
		return errors.New("未启用主密码")
	}
	if password == "" {
		return ErrBadPassword
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil || len(salt) == 0 {
		return errors.New("安全元数据损坏")
	}
	key := DeriveKey(password, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(key, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			return ErrBadPassword
		}
	}
	v.key = key
	v.locked = false
	return nil
}

// EnableMasterPassword 开启主密码模式：用新密码派生密钥并重加密校验器。
// 调用方需在之后用 ReencryptAll 把存量字段迁到新密钥。
// 返回旧密钥（可能为空）与新密钥，供迁移使用。
func (v *Vault) EnableMasterPassword(password string) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if password == "" {
		return nil, nil, errors.New("主密码不能为空")
	}
	if v.meta.MasterPasswordEnabled {
		return nil, nil, errors.New("主密码已启用")
	}
	if v.key == nil {
		return nil, nil, ErrLocked
	}
	oldKey = append([]byte(nil), v.key...)
	salt, err := NewSalt()
	if err != nil {
		return nil, nil, err
	}
	newKey = DeriveKey(password, salt)
	verifier, err := Encrypt(newKey, "ding-ssh-ok")
	if err != nil {
		return nil, nil, err
	}
	v.meta.MasterPasswordEnabled = true
	v.meta.SaltB64 = base64.StdEncoding.EncodeToString(salt)
	v.meta.VerifierB64 = verifier
	if err := v.saveMeta(); err != nil {
		return nil, nil, err
	}
	v.key = newKey
	// 清除 Keyring / 文件密钥，避免旁路。
	_ = keyring.Delete(keyringService, keyringUser)
	_ = os.Remove(v.fileKeyPath())
	return oldKey, newKey, nil
}

// DisableMasterPassword 关闭主密码，改回 Keyring/文件密钥。
func (v *Vault) DisableMasterPassword(password string) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.meta.MasterPasswordEnabled {
		return nil, nil, errors.New("主密码未启用")
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil {
		return nil, nil, errors.New("安全元数据损坏")
	}
	derived := DeriveKey(password, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(derived, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			return nil, nil, ErrBadPassword
		}
	}
	oldKey = append([]byte(nil), v.key...)
	if len(oldKey) == 0 {
		oldKey = derived
	}
	newKey, err = NewMasterKey()
	if err != nil {
		return nil, nil, err
	}
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(newKey)); err != nil {
		if err2 := os.WriteFile(v.fileKeyPath(), []byte(base64.StdEncoding.EncodeToString(newKey)), 0o600); err2 != nil {
			return nil, nil, fmt.Errorf("保存主密钥失败: %v / %v", err, err2)
		}
	}
	v.meta.MasterPasswordEnabled = false
	v.meta.SaltB64 = ""
	v.meta.VerifierB64 = ""
	if err := v.saveMeta(); err != nil {
		return nil, nil, err
	}
	v.key = newKey
	v.locked = false
	return oldKey, newKey, nil
}

// ChangeMasterPassword 更换主密码。
func (v *Vault) ChangeMasterPassword(oldPass, newPass string) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.meta.MasterPasswordEnabled {
		return nil, nil, errors.New("主密码未启用")
	}
	if newPass == "" {
		return nil, nil, errors.New("新主密码不能为空")
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil {
		return nil, nil, errors.New("安全元数据损坏")
	}
	oldKey = DeriveKey(oldPass, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(oldKey, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			return nil, nil, ErrBadPassword
		}
	}
	newSalt, err := NewSalt()
	if err != nil {
		return nil, nil, err
	}
	newKey = DeriveKey(newPass, newSalt)
	verifier, err := Encrypt(newKey, "ding-ssh-ok")
	if err != nil {
		return nil, nil, err
	}
	v.meta.SaltB64 = base64.StdEncoding.EncodeToString(newSalt)
	v.meta.VerifierB64 = verifier
	if err := v.saveMeta(); err != nil {
		return nil, nil, err
	}
	v.key = newKey
	v.locked = false
	return oldKey, newKey, nil
}

// EncryptField 加密敏感字段；空串原样返回；已加密则原样返回。
func (v *Vault) EncryptField(plain string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if plain == "" {
		return "", nil
	}
	if IsEncrypted(plain) {
		return plain, nil
	}
	if v.key == nil || v.locked {
		return "", ErrLocked
	}
	return Encrypt(v.key, plain)
}

// DecryptField 解密敏感字段；明文原样返回（兼容迁移前数据）。
func (v *Vault) DecryptField(value string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if value == "" {
		return "", nil
	}
	if !IsEncrypted(value) {
		return value, nil
	}
	if v.key == nil || v.locked {
		return "", ErrLocked
	}
	return Decrypt(v.key, value)
}

// MustDecrypt 解密失败时返回空串（列表展示用）。
func (v *Vault) MustDecrypt(value string) string {
	out, err := v.DecryptField(value)
	if err != nil {
		return ""
	}
	return out
}

// Key 返回当前主密钥副本（仅供字段重加密迁移）。
func (v *Vault) Key() ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.key == nil || v.locked {
		return nil, ErrLocked
	}
	return append([]byte(nil), v.key...), nil
}

// MarkMigrated 记录完成明文迁移时间。
func (v *Vault) MarkMigrated() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.meta.MigratedAt = time.Now().UnixMilli()
	return v.saveMeta()
}

// NeedsMigration 是否尚未完成明文→密文迁移。
func (v *Vault) NeedsMigration() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.meta.MigratedAt == 0
}
