// Package cryptox 提供 AES-256-GCM 字段加密与 Argon2id 密钥派生。
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"ding-ssh/internal/i18n"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"ding-ssh/internal/logx"

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
	ErrLocked       = errors.New(i18n.T("保险库未解锁"))
	ErrBadPassword  = errors.New(i18n.T("主密码错误"))
	ErrNotEncrypted = errors.New(i18n.T("不是加密字段"))
)

// Encrypt 使用 AES-256-GCM 加密明文，返回带 Prefix 的密文字符串。
func Encrypt(key []byte, plaintext string) (string, error) {
	logx.Debugf("[cryptox] 加密字段: plaintextLen=%d keyLen=%d", len(plaintext), len(key))
	if len(key) != 32 {
		logx.Warnf("[cryptox] 加密字段被拒绝: 密钥长度非法 keyLen=%d", len(key))
		return "", fmt.Errorf("%s", i18n.T("密钥长度必须为 32 字节"))
	}
	if plaintext == "" {
		logx.Debugf("[cryptox] 加密字段为空，原样返回空串")
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		logx.Warnf("[cryptox] 创建 AES 加密器失败: err=%v", err)
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logx.Warnf("[cryptox] 创建 GCM 模式失败: err=%v", err)
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		logx.Warnf("[cryptox] 生成加密随机 nonce 失败: err=%v", err)
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	logx.Debugf("[cryptox] 加密字段完成: plaintextLen=%d cipherBytes=%d", len(plaintext), len(sealed))
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密带 Prefix 的密文；若非加密字段返回 ErrNotEncrypted。
func Decrypt(key []byte, ciphertext string) (string, error) {
	logx.Debugf("[cryptox] 解密字段: ciphertextLen=%d encrypted=%v keyLen=%d",
		len(ciphertext), IsEncrypted(ciphertext), len(key))
	if ciphertext == "" {
		logx.Debugf("[cryptox] 解密字段为空，原样返回空串")
		return "", nil
	}
	if !IsEncrypted(ciphertext) {
		logx.Debugf("[cryptox] 字段不是密文，返回 ErrNotEncrypted")
		return "", ErrNotEncrypted
	}
	if len(key) != 32 {
		logx.Warnf("[cryptox] 解密字段被拒绝: 密钥长度非法 keyLen=%d", len(key))
		return "", fmt.Errorf("%s", i18n.T("密钥长度必须为 32 字节"))
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, Prefix))
	if err != nil {
		logx.Warnf("[cryptox] 密文 base64 解码失败: ciphertextLen=%d err=%v", len(ciphertext), err)
		return "", fmt.Errorf(i18n.T("密文解码失败: %w"), err)
	}
	if len(raw) < nonceLen+1 {
		logx.Warnf("[cryptox] 密文过短，无法解密: rawBytes=%d", len(raw))
		return "", errors.New(i18n.T("密文过短"))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		logx.Warnf("[cryptox] 创建 AES 解密器失败: err=%v", err)
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logx.Warnf("[cryptox] 创建 GCM 模式失败: err=%v", err)
		return "", err
	}
	nonce, sealed := raw[:nonceLen], raw[nonceLen:]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		logx.Warnf("[cryptox] 字段解密失败（密钥不匹配或密文被篡改）: rawBytes=%d", len(raw))
		return "", errors.New(i18n.T("解密失败"))
	}
	logx.Debugf("[cryptox] 解密字段完成: plaintextLen=%d", len(plain))
	return string(plain), nil
}

// IsEncrypted 判断字段是否已加密。
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, Prefix)
}

// DeriveKey 使用 Argon2id 从密码与盐派生 32 字节密钥。
func DeriveKey(password string, salt []byte) []byte {
	start := time.Now()
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	// Argon2id 是刻意昂贵的操作（约 64MiB 内存），记录耗时便于判断解锁/迁移是否卡顿。
	logx.Debugf("[cryptox] Argon2id 密钥派生完成: saltLen=%d keyLen=%d elapsed=%s",
		len(salt), len(key), time.Since(start))
	return key
}

// RandomBytes 生成加密安全的随机字节。
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		logx.Warnf("[cryptox] 生成随机字节失败: n=%d err=%v", n, err)
		return nil, err
	}
	logx.Debugf("[cryptox] 已生成随机字节: n=%d", n)
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
	equal := subtle.ConstantTimeCompare(a, b) == 1
	logx.Debugf("[cryptox] 比较主密钥: equal=%v", equal)
	return equal
}

// SealPayload 用密码加密任意字节载荷（dingpack 等），格式：salt(16) || nonce(12) || ciphertext。
func SealPayload(password string, plaintext []byte) ([]byte, error) {
	start := time.Now()
	logx.Debugf("[cryptox] 加密载荷: plaintextBytes=%d", len(plaintext))
	salt, err := NewSalt()
	if err != nil {
		logx.Warnf("[cryptox] 加密载荷生成盐失败: err=%v", err)
		return nil, err
	}
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		logx.Warnf("[cryptox] 加密载荷创建 AES 失败: err=%v", err)
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logx.Warnf("[cryptox] 加密载荷创建 GCM 失败: err=%v", err)
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		logx.Warnf("[cryptox] 加密载荷生成 nonce 失败: err=%v", err)
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(salt)+len(nonce)+len(sealed))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	logx.Debugf("[cryptox] 加密载荷完成: plaintextBytes=%d blobBytes=%d elapsed=%s",
		len(plaintext), len(out), time.Since(start))
	return out, nil
}

// OpenPayload 用密码解密 SealPayload 产物。
func OpenPayload(password string, blob []byte) ([]byte, error) {
	start := time.Now()
	logx.Debugf("[cryptox] 解密载荷: blobBytes=%d", len(blob))
	if len(blob) < saltLen+nonceLen+1 {
		logx.Warnf("[cryptox] 解密载荷被拒绝: 数据包过短 blobBytes=%d", len(blob))
		return nil, errors.New(i18n.T("数据包损坏或过短"))
	}
	salt := blob[:saltLen]
	nonce := blob[saltLen : saltLen+nonceLen]
	sealed := blob[saltLen+nonceLen:]
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		logx.Warnf("[cryptox] 解密载荷创建 AES 失败: err=%v", err)
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		logx.Warnf("[cryptox] 解密载荷创建 GCM 失败: err=%v", err)
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		logx.Warnf("[cryptox] 解密载荷失败（密码错误或数据损坏）: blobBytes=%d", len(blob))
		return nil, ErrBadPassword
	}
	logx.Debugf("[cryptox] 解密载荷完成: plaintextBytes=%d elapsed=%s", len(plain), time.Since(start))
	return plain, nil
}
