package cryptox

import (
	"fmt"
)

// FieldCipher 供 store 层注入的加解密接口。
type FieldCipher interface {
	EncryptField(plain string) (string, error)
	DecryptField(value string) (string, error)
}

// Reencrypt 用新密钥重加密字段（旧密文用 oldKey 解，新密文用 newKey 封）。
func Reencrypt(oldKey, newKey []byte, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	var plain string
	var err error
	if IsEncrypted(value) {
		plain, err = Decrypt(oldKey, value)
		if err != nil {
			return "", fmt.Errorf("旧密钥解密失败: %w", err)
		}
	} else {
		plain = value
	}
	return Encrypt(newKey, plain)
}

// SealPlain 确保明文被加密（已加密则跳过）。
func SealPlain(cipher FieldCipher, plain string) (string, error) {
	if cipher == nil || plain == "" {
		return plain, nil
	}
	return cipher.EncryptField(plain)
}

// OpenField 解密字段；cipher 为空时原样返回。
func OpenField(cipher FieldCipher, value string) (string, error) {
	if cipher == nil || value == "" {
		return value, nil
	}
	return cipher.DecryptField(value)
}
