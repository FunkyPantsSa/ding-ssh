package cryptox

import (
	"ding-ssh/internal/i18n"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
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
	SaltB64               string `json:"saltB64,omitempty"`     // Argon2 盐（主密码模式）
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
	mu       sync.RWMutex
	key      []byte
	meta     Meta
	dir      string
	locked   bool // true 表示需要主密码解锁
	isolated bool // Captured at open; isolated instances never access the real keychain.
}

// OpenVault 加载安全元数据并尝试自动解锁（Keyring 模式）。
func OpenVault(configDir string) (*Vault, error) {
	start := time.Now()
	if configDir == "" {
		configDir = appdir.ConfigDir()
	}
	logx.Infof("[cryptox] 打开安全保险库: dir=%q", configDir)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		logx.Errorf("[cryptox] 创建保险库目录失败: dir=%q err=%v", configDir, err)
		return nil, err
	}
	v := &Vault{dir: configDir, isolated: appdir.IsDevOverride()}
	if err := v.loadMeta(); err != nil {
		logx.Errorf("[cryptox] 读取保险库元数据失败: dir=%q err=%v", configDir, err)
		return nil, err
	}
	if v.meta.MasterPasswordEnabled {
		v.locked = true
		logx.Infof("[cryptox] 保险库已锁定，等待主密码解锁: elapsed=%s", time.Since(start))
		return v, nil
	}
	// 默认模式：从 Keyring 取主密钥，失败则生成并写入。
	var key []byte
	var err error
	if v.isolated {
		// Isolated test instances must not read or create the user's real
		// keychain entry; keep their generated key inside the override dir.
		logx.Debugf("[cryptox] 检测到开发配置目录覆盖，主密钥改用本地文件: dir=%q", configDir)
		key, err = v.loadOrCreateFileKey()
	} else {
		key, err = v.loadOrCreateKeyringKey()
	}
	if err != nil {
		// Keyring 不可用时回退到本地受保护文件（仍优于明文）。
		logx.Warnf("[cryptox] Keyring 主密钥不可用，回退本地密钥文件: err=%v", err)
		key, err = v.loadOrCreateFileKey()
		if err != nil {
			logx.Errorf("[cryptox] 保险库解锁失败，无法获取主密钥: dir=%q err=%v", configDir, err)
			return nil, err
		}
		logx.Warnf("[cryptox] 保险库已回退到本地密钥文件模式: dir=%q", configDir)
	}
	v.key = key
	v.locked = false
	logx.Infof("[cryptox] 保险库已解锁: mode=%s elapsed=%s", v.unlockMode(), time.Since(start))
	return v, nil
}

// unlockMode 返回当前解锁模式（仅用于日志）。
func (v *Vault) unlockMode() string {
	if v.meta.MasterPasswordEnabled {
		return "master-password"
	}
	if v.isolated {
		return "file-key(dev)"
	}
	return "keyring-or-file"
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
		logx.Debugf("[cryptox] 保险库元数据不存在，按默认值处理: path=%q", v.metaPath())
		return nil
	}
	if err != nil {
		logx.Warnf("[cryptox] 读取保险库元数据文件失败: path=%q err=%v", v.metaPath(), err)
		return err
	}
	if err := json.Unmarshal(data, &v.meta); err != nil {
		logx.Warnf("[cryptox] 解析保险库元数据失败: path=%q bytes=%d err=%v", v.metaPath(), len(data), err)
		return err
	}
	if v.meta.MasterPasswordEnabled {
		salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
		if err != nil || len(salt) != saltLen || !IsEncrypted(v.meta.VerifierB64) {
			return errors.New(i18n.T("安全元数据损坏"))
		}
	}
	logx.Debugf("[cryptox] 保险库元数据已读取: masterPassword=%v needsMigration=%v migratedAt=%d",
		v.meta.MasterPasswordEnabled, v.meta.MigratedAt == 0, v.meta.MigratedAt)
	return nil
}

func (v *Vault) saveMeta() error {
	data, err := json.MarshalIndent(v.meta, "", "  ")
	if err != nil {
		logx.Warnf("[cryptox] 序列化保险库元数据失败: err=%v", err)
		return err
	}
	tmp := v.metaPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logx.Warnf("[cryptox] 写入保险库元数据临时文件失败: path=%q err=%v", tmp, err)
		return err
	}
	if err := os.Rename(tmp, v.metaPath()); err != nil {
		logx.Warnf("[cryptox] 原子替换保险库元数据失败: path=%q err=%v", v.metaPath(), err)
		return err
	}
	logx.Debugf("[cryptox] 保险库元数据已保存: path=%q bytes=%d", v.metaPath(), len(data))
	return nil
}

func (v *Vault) loadOrCreateKeyringKey() ([]byte, error) {
	secret, err := keyring.Get(keyringService, keyringUser)
	if err == nil && secret != "" {
		key, err := base64.StdEncoding.DecodeString(secret)
		if err != nil || len(key) != 32 {
			logx.Warnf("[cryptox] Keyring 中的主密钥无效: keyLen=%d err=%v", len(key), err)
			return nil, fmt.Errorf("%s", i18n.T("Keyring 中的主密钥无效"))
		}
		logx.Debugf("[cryptox] 已从 Keyring 读取主密钥: keyLen=%d", len(key))
		return key, nil
	}
	if err != nil {
		logx.Debugf("[cryptox] Keyring 中没有可用主密钥，将生成新密钥: err=%v", err)
	}
	key, err := NewMasterKey()
	if err != nil {
		logx.Warnf("[cryptox] 生成新主密钥失败: err=%v", err)
		return nil, err
	}
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(key)); err != nil {
		logx.Warnf("[cryptox] 写入 Keyring 主密钥失败: err=%v", err)
		return nil, err
	}
	logx.Infof("[cryptox] 已在 Keyring 中创建主密钥: keyLen=%d", len(key))
	return key, nil
}

func (v *Vault) loadOrCreateFileKey() ([]byte, error) {
	path := v.fileKeyPath()
	data, err := os.ReadFile(path)
	if err == nil {
		key, derr := base64.StdEncoding.DecodeString(string(data))
		if derr == nil && len(key) == 32 {
			logx.Debugf("[cryptox] 已从本地文件读取主密钥: path=%q", path)
			return key, nil
		}
		// Never replace an existing key with a random one: doing so makes all
		// previously encrypted fields permanently unreadable.
		return nil, errors.New(i18n.T("安全元数据损坏"))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := NewMasterKey()
	if err != nil {
		logx.Warnf("[cryptox] 生成主密钥失败: err=%v", err)
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		logx.Warnf("[cryptox] 写入本地主密钥文件失败: path=%q err=%v", path, err)
		return nil, err
	}
	logx.Infof("[cryptox] 已创建本地主密钥文件: path=%q", path)
	return key, nil
}

// Status 返回当前安全状态。
func (v *Vault) Status() Status {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keyringOK := true
	if v.isolated {
		keyringOK = false
	} else if _, err := keyring.Get(keyringService, keyringUser); err != nil && !v.meta.MasterPasswordEnabled {
		// 探测：写入测试可能过重，仅标记可用性近似值
		keyringOK = errors.Is(err, keyring.ErrNotFound) || v.key != nil
	}
	st := Status{
		Unlocked:              !v.locked && v.key != nil,
		MasterPasswordEnabled: v.meta.MasterPasswordEnabled,
		KeyringAvailable:      keyringOK,
		NeedsUnlock:           v.locked,
	}
	logx.Debugf("[cryptox] 查询保险库状态: unlocked=%v masterPassword=%v keyring=%v needsUnlock=%v",
		st.Unlocked, st.MasterPasswordEnabled, st.KeyringAvailable, st.NeedsUnlock)
	return st
}

// Unlock 使用主密码解锁。
func (v *Vault) Unlock(password string) error {
	start := time.Now()
	v.mu.Lock()
	defer v.mu.Unlock()
	logx.Debugf("[cryptox] 尝试用主密码解锁保险库")
	if !v.meta.MasterPasswordEnabled {
		logx.Warnf("[cryptox] 解锁保险库失败: 未启用主密码")
		return errors.New(i18n.T("未启用主密码"))
	}
	if password == "" {
		logx.Warnf("[cryptox] 解锁保险库失败: 主密码为空")
		return ErrBadPassword
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil || len(salt) != saltLen || !IsEncrypted(v.meta.VerifierB64) {
		logx.Errorf("[cryptox] 解锁保险库失败: 安全元数据中的盐损坏 err=%v saltLen=%d", err, len(salt))
		return errors.New(i18n.T("安全元数据损坏"))
	}
	key := DeriveKey(password, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(key, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			logx.Warnf("[cryptox] 解锁保险库失败: 主密码校验不通过")
			return ErrBadPassword
		}
	}
	v.key = key
	v.locked = false
	logx.Infof("[cryptox] 保险库主密码解锁成功: elapsed=%s", time.Since(start))
	return nil
}

// EnableMasterPassword 开启主密码模式：用新密码派生密钥并重加密校验器。
// 调用方需在之后用 ReencryptAll 把存量字段迁到新密钥。
// 返回旧密钥（可能为空）与新密钥，供迁移使用。
func (v *Vault) EnableMasterPassword(password string) (oldKey, newKey []byte, err error) {
	return v.EnableMasterPasswordWithMigration(password, nil)
}

// KeyMigration must leave stored fields unchanged when it returns an error.
type KeyMigration func(oldKey, newKey []byte) error

// commitKeyChange runs with v.mu held. Failed field migration restores the
// previous on-disk metadata before releasing readers; the active key is only
// published after both metadata and the migration have succeeded.
func (v *Vault) commitKeyChange(next Meta, oldKey, newKey []byte, migrate KeyMigration) error {
	previous := v.meta
	v.meta = next
	if err := v.saveMeta(); err != nil {
		v.meta = previous
		return err
	}
	if migrate != nil {
		if err := migrate(oldKey, newKey); err != nil {
			v.meta = previous
			return errors.Join(err, v.saveMeta())
		}
	}
	v.key = newKey
	v.locked = false
	return nil
}

func (v *Vault) EnableMasterPasswordWithMigration(password string, migrate KeyMigration) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	logx.Infof("[cryptox] 开始启用主密码模式")
	if password == "" {
		logx.Warnf("[cryptox] 启用主密码被拒绝: 主密码为空")
		return nil, nil, errors.New(i18n.T("主密码不能为空"))
	}
	if v.meta.MasterPasswordEnabled {
		logx.Warnf("[cryptox] 启用主密码被拒绝: 主密码已启用")
		return nil, nil, errors.New(i18n.T("主密码已启用"))
	}
	if v.key == nil {
		logx.Warnf("[cryptox] 启用主密码被拒绝: 保险库未解锁")
		return nil, nil, ErrLocked
	}
	oldKey = append([]byte(nil), v.key...)
	salt, err := NewSalt()
	if err != nil {
		logx.Errorf("[cryptox] 启用主密码生成盐失败: err=%v", err)
		return nil, nil, err
	}
	newKey = DeriveKey(password, salt)
	verifier, err := Encrypt(newKey, "ding-ssh-ok")
	if err != nil {
		logx.Errorf("[cryptox] 启用主密码生成校验器失败: err=%v", err)
		return nil, nil, err
	}
	next := v.meta
	next.MasterPasswordEnabled = true
	next.SaltB64 = base64.StdEncoding.EncodeToString(salt)
	next.VerifierB64 = verifier
	if err := v.commitKeyChange(next, oldKey, newKey, migrate); err != nil {
		logx.Errorf("[cryptox] 启用主密码保存元数据失败: err=%v", err)
		return nil, nil, err
	}
	// 清除 Keyring / 文件密钥，避免旁路。
	if !v.isolated {
		if err := keyring.Delete(keyringService, keyringUser); err != nil {
			logx.Debugf("[cryptox] 清理 Keyring 主密钥未成功（可能本就不存在）: err=%v", err)
		}
	}
	if err := os.Remove(v.fileKeyPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		logx.Debugf("[cryptox] 清理本地主密钥文件未成功: path=%q err=%v", v.fileKeyPath(), err)
	}
	logx.Infof("[cryptox] 主密码模式已启用: 存量字段需调用方重加密")
	return oldKey, newKey, nil
}

// DisableMasterPassword 关闭主密码，改回 Keyring/文件密钥。
func (v *Vault) DisableMasterPassword(password string) (oldKey, newKey []byte, err error) {
	return v.DisableMasterPasswordWithMigration(password, nil)
}

func (v *Vault) DisableMasterPasswordWithMigration(password string, migrate KeyMigration) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	logx.Infof("[cryptox] 开始关闭主密码模式")
	if !v.meta.MasterPasswordEnabled {
		logx.Warnf("[cryptox] 关闭主密码被拒绝: 主密码未启用")
		return nil, nil, errors.New(i18n.T("主密码未启用"))
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil || len(salt) != saltLen || !IsEncrypted(v.meta.VerifierB64) {
		logx.Errorf("[cryptox] 关闭主密码失败: 安全元数据中的盐损坏 err=%v", err)
		return nil, nil, errors.New(i18n.T("安全元数据损坏"))
	}
	derived := DeriveKey(password, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(derived, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			logx.Warnf("[cryptox] 关闭主密码失败: 主密码校验不通过")
			return nil, nil, ErrBadPassword
		}
	}
	oldKey = append([]byte(nil), v.key...)
	if len(oldKey) == 0 {
		oldKey = derived
	}
	newKey, err = NewMasterKey()
	if err != nil {
		logx.Errorf("[cryptox] 关闭主密码生成新主密钥失败: err=%v", err)
		return nil, nil, err
	}
	var keyErr error
	if !v.isolated {
		keyErr = keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(newKey))
	}
	if v.isolated || keyErr != nil {
		if keyErr != nil {
			logx.Warnf("[cryptox] 关闭主密码写入 Keyring 失败，回退本地密钥文件: err=%v", keyErr)
		}
		if err2 := os.WriteFile(v.fileKeyPath(), []byte(base64.StdEncoding.EncodeToString(newKey)), 0o600); err2 != nil {
			logx.Errorf("[cryptox] 关闭主密码保存主密钥失败: keyringErr=%v fileErr=%v", keyErr, err2)
			return nil, nil, fmt.Errorf(i18n.T("保存主密钥失败: %v / %v"), keyErr, err2)
		}
	}
	next := v.meta
	next.MasterPasswordEnabled = false
	next.SaltB64 = ""
	next.VerifierB64 = ""
	if err := v.commitKeyChange(next, oldKey, newKey, migrate); err != nil {
		logx.Errorf("[cryptox] 关闭主密码保存元数据失败: err=%v", err)
		return nil, nil, err
	}
	logx.Infof("[cryptox] 主密码模式已关闭，改回 Keyring/文件密钥")
	return oldKey, newKey, nil
}

// ChangeMasterPassword 更换主密码。
func (v *Vault) ChangeMasterPassword(oldPass, newPass string) (oldKey, newKey []byte, err error) {
	return v.ChangeMasterPasswordWithMigration(oldPass, newPass, nil)
}

func (v *Vault) ChangeMasterPasswordWithMigration(oldPass, newPass string, migrate KeyMigration) (oldKey, newKey []byte, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	logx.Infof("[cryptox] 开始更换主密码")
	if !v.meta.MasterPasswordEnabled {
		logx.Warnf("[cryptox] 更换主密码被拒绝: 主密码未启用")
		return nil, nil, errors.New(i18n.T("主密码未启用"))
	}
	if newPass == "" {
		logx.Warnf("[cryptox] 更换主密码被拒绝: 新主密码为空")
		return nil, nil, errors.New(i18n.T("新主密码不能为空"))
	}
	salt, err := base64.StdEncoding.DecodeString(v.meta.SaltB64)
	if err != nil || len(salt) != saltLen || !IsEncrypted(v.meta.VerifierB64) {
		logx.Errorf("[cryptox] 更换主密码失败: 安全元数据中的盐损坏 err=%v", err)
		return nil, nil, errors.New(i18n.T("安全元数据损坏"))
	}
	oldKey = DeriveKey(oldPass, salt)
	if v.meta.VerifierB64 != "" {
		plain, err := Decrypt(oldKey, v.meta.VerifierB64)
		if err != nil || plain != "ding-ssh-ok" {
			logx.Warnf("[cryptox] 更换主密码失败: 旧主密码校验不通过")
			return nil, nil, ErrBadPassword
		}
	}
	newSalt, err := NewSalt()
	if err != nil {
		logx.Errorf("[cryptox] 更换主密码生成新盐失败: err=%v", err)
		return nil, nil, err
	}
	newKey = DeriveKey(newPass, newSalt)
	verifier, err := Encrypt(newKey, "ding-ssh-ok")
	if err != nil {
		logx.Errorf("[cryptox] 更换主密码生成校验器失败: err=%v", err)
		return nil, nil, err
	}
	next := v.meta
	next.SaltB64 = base64.StdEncoding.EncodeToString(newSalt)
	next.VerifierB64 = verifier
	if err := v.commitKeyChange(next, oldKey, newKey, migrate); err != nil {
		logx.Errorf("[cryptox] 更换主密码保存元数据失败: err=%v", err)
		return nil, nil, err
	}
	logx.Infof("[cryptox] 主密码已更换: 存量字段需调用方重加密")
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
		logx.Debugf("[cryptox] 字段已是密文，加密跳过: valueLen=%d", len(plain))
		return plain, nil
	}
	if v.key == nil || v.locked {
		logx.Warnf("[cryptox] 加密字段失败: 保险库未解锁")
		return "", ErrLocked
	}
	logx.Debugf("[cryptox] 加密敏感字段: plaintextLen=%d", len(plain))
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
		logx.Debugf("[cryptox] 字段为明文（迁移前数据），解密跳过: valueLen=%d", len(value))
		return value, nil
	}
	if v.key == nil || v.locked {
		logx.Warnf("[cryptox] 解密字段失败: 保险库未解锁")
		return "", ErrLocked
	}
	out, err := Decrypt(v.key, value)
	if err != nil {
		logx.Warnf("[cryptox] 解密敏感字段失败: valueLen=%d err=%v", len(value), err)
		return "", err
	}
	logx.Debugf("[cryptox] 解密敏感字段完成: plaintextLen=%d", len(out))
	return out, nil
}

// MustDecrypt 解密失败时返回空串（列表展示用）。
func (v *Vault) MustDecrypt(value string) string {
	out, err := v.DecryptField(value)
	if err != nil {
		// 展示路径静默降级为空串，属于数据不可读的降级，需要留下可排查的 WARN。
		logx.Warnf("[cryptox] 展示用解密失败，回落空串: valueLen=%d err=%v", len(value), err)
		return ""
	}
	return out
}

// Key 返回当前主密钥副本（仅供字段重加密迁移）。
func (v *Vault) Key() ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.key == nil || v.locked {
		logx.Warnf("[cryptox] 读取主密钥失败: 保险库未解锁")
		return nil, ErrLocked
	}
	logx.Debugf("[cryptox] 已导出主密钥副本: keyLen=%d", len(v.key))
	return append([]byte(nil), v.key...), nil
}

// MarkMigrated 记录完成明文迁移时间。
func (v *Vault) MarkMigrated() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.meta.MigratedAt = time.Now().UnixMilli()
	if err := v.saveMeta(); err != nil {
		logx.Errorf("[cryptox] 记录迁移完成时间失败: err=%v", err)
		return err
	}
	logx.Infof("[cryptox] 已记录明文迁移完成时间: migratedAt=%d", v.meta.MigratedAt)
	return nil
}

// NeedsMigration 是否尚未完成明文→密文迁移。
func (v *Vault) NeedsMigration() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	need := v.meta.MigratedAt == 0
	logx.Debugf("[cryptox] 查询明文迁移状态: needsMigration=%v", need)
	return need
}
