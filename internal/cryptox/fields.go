package cryptox

import (
	"ding-ssh/internal/i18n"
	"fmt"

	"ding-ssh/internal/logx"
)

// FieldCipher 供 store 层注入的加解密接口。
type FieldCipher interface {
	EncryptField(plain string) (string, error)
	DecryptField(value string) (string, error)
}

// BeginFieldOperation holds one key generation until the caller has finished
// all fields and its SQL read/write. Per-field locks alone cannot prevent a
// rotation between encryption and persistence, or between two scanned fields.
func BeginFieldOperation(cipher FieldCipher) func() {
	if locker, ok := cipher.(interface{ BeginFieldOperation() func() }); ok {
		return locker.BeginFieldOperation()
	}
	return func() {}
}

func (v *Vault) BeginFieldOperation() func() {
	v.operations.RLock()
	return v.operations.RUnlock
}

// Reencrypt 用新密钥重加密字段（旧密文用 oldKey 解，新密文用 newKey 封）。
func Reencrypt(oldKey, newKey []byte, value string) (string, error) {
	if value == "" {
		logx.Debugf("[cryptox] 重加密字段为空，跳过")
		return "", nil
	}
	logx.Debugf("[cryptox] 重加密字段: encrypted=%v valueLen=%d oldKeyLen=%d newKeyLen=%d",
		IsEncrypted(value), len(value), len(oldKey), len(newKey))
	var plain string
	var err error
	if IsEncrypted(value) {
		plain, err = Decrypt(oldKey, value)
		if err != nil {
			logx.Warnf("[cryptox] 重加密字段时旧密钥解密失败: valueLen=%d err=%v", len(value), err)
			return "", fmt.Errorf(i18n.T("旧密钥解密失败: %w"), err)
		}
	} else {
		plain = value
		logx.Debugf("[cryptox] 重加密字段原为明文，直接以新密钥加密: valueLen=%d", len(value))
	}
	return Encrypt(newKey, plain)
}

// SealPlain 确保明文被加密（已加密则跳过）。
func SealPlain(cipher FieldCipher, plain string) (string, error) {
	if cipher == nil {
		logx.Debugf("[cryptox] 未配置加密器，明文原样落库: valueLen=%d", len(plain))
		return plain, nil
	}
	if plain == "" {
		return plain, nil
	}
	return cipher.EncryptField(plain)
}

// OpenField 解密字段；cipher 为空时原样返回。
func OpenField(cipher FieldCipher, value string) (string, error) {
	if cipher == nil {
		logx.Debugf("[cryptox] 未配置加密器，字段原样返回: valueLen=%d", len(value))
		return value, nil
	}
	if value == "" {
		return value, nil
	}
	return cipher.DecryptField(value)
}
