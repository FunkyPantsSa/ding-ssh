// Package cryptox 提供 AES-256-GCM 字段加密与 Argon2id 密钥派生。
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// Prefix 加密字段前缀，用于识别密文与明文迁移。
	Prefix = "enc1:"

	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
	nonceLen     = 12
)

var (
	ErrLocked       = errors.New("保险库未解锁")
	ErrBadPassword  = errors.New("主密码错误")
	ErrNotEncrypted = errors.New("不是加密字段")
)

// Encrypt 使用 AES-256-GCM 加密明文，返回带 Prefix 的密文字符串。
func Encrypt(key []byte, plaintext string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("密钥长度必须为 32 字节")
	}
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密带 Prefix 的密文；若非加密字段返回 ErrNotEncrypted。
func Decrypt(key []byte, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !IsEncrypted(ciphertext) {
		return "", ErrNotEncrypted
	}
	if len(key) != 32 {
		return "", fmt.Errorf("密钥长度必须为 32 字节")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, Prefix))
	if err != nil {
		return "", fmt.Errorf("密文解码失败: %w", err)
	}
	if len(raw) < nonceLen+1 {
		return "", errors.New("密文过短")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, sealed := raw[:nonceLen], raw[nonceLen:]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", errors.New("解密失败")
	}
	return string(plain), nil
}

// IsEncrypted 判断字段是否已加密。
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, Prefix)
}

// DeriveKey 使用 Argon2id 从密码与盐派生 32 字节密钥。
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// RandomBytes 生成加密安全的随机字节。
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	return b, nil
}

// NewSalt 生成 Argon2 盐。
func NewSalt() ([]byte, error) {
	return RandomBytes(saltLen)
}

// NewMasterKey 生成随机 32 字节主密钥。
func NewMasterKey() ([]byte, error) {
	return RandomBytes(32)
}

// EqualKey 常量时间比较两把密钥。
func EqualKey(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// SealPayload 用密码加密任意字节载荷（dingpack 等），格式：salt(16) || nonce(12) || ciphertext。
func SealPayload(password string, plaintext []byte) ([]byte, error) {
	salt, err := NewSalt()
	if err != nil {
		return nil, err
	}
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(salt)+len(nonce)+len(sealed))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// OpenPayload 用密码解密 SealPayload 产物。
func OpenPayload(password string, blob []byte) ([]byte, error) {
	if len(blob) < saltLen+nonceLen+1 {
		return nil, errors.New("数据包损坏或过短")
	}
	salt := blob[:saltLen]
	nonce := blob[saltLen : saltLen+nonceLen]
	sealed := blob[saltLen+nonceLen:]
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrBadPassword
	}
	return plain, nil
}
