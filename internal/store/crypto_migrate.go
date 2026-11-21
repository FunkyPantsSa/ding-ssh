package store

import (
	"database/sql"
	"ding-ssh/internal/i18n"
	"fmt"
	"time"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/logx"
)

// MigratePlaintextSecrets 将 servers/credentials 中的明文 password/key_content 加密落库。
func MigratePlaintextSecrets(db *sql.DB, cipher cryptox.FieldCipher) (int, error) {
	if db == nil || cipher == nil {
		logx.Debugf("[store] 跳过明文敏感字段迁移: db=%v cipher=%v", db != nil, cipher != nil)
		return 0, nil
	}
	start := time.Now()
	logx.Infof("[store] 开始明文敏感字段加密迁移")
	n := 0
	rows, err := db.Query(`SELECT id, password, key_content FROM servers`)
	if err != nil {
		logx.Errorf("[store] 明文迁移查询服务器失败: err=%v", err)
		return 0, err
	}
	type row struct {
		id, password, keyContent string
	}
	var servers []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			rows.Close()
			logx.Errorf("[store] 明文迁移扫描服务器行失败: err=%v", err)
			return n, err
		}
		servers = append(servers, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 明文迁移遍历服务器失败: err=%v", err)
		return n, err
	}
	logx.Debugf("[store] 明文迁移待检查服务器: count=%d", len(servers))
	for _, r := range servers {
		pw, kc := r.password, r.keyContent
		pwChanged, kcChanged := false, false
		if pw != "" && !cryptox.IsEncrypted(pw) {
			enc, err := cipher.EncryptField(pw)
			if err != nil {
				logx.Errorf("[store] 明文迁移加密服务器密码失败: id=%s err=%v", r.id, err)
				return n, err
			}
			pw = enc
			pwChanged = true
		}
		if kc != "" && !cryptox.IsEncrypted(kc) {
			enc, err := cipher.EncryptField(kc)
			if err != nil {
				logx.Errorf("[store] 明文迁移加密服务器私钥失败: id=%s err=%v", r.id, err)
				return n, err
			}
			kc = enc
			kcChanged = true
		}
		if !pwChanged && !kcChanged {
			continue
		}
		if _, err := db.Exec(`UPDATE servers SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			logx.Errorf("[store] 明文迁移写回服务器敏感字段失败: id=%s err=%v", r.id, err)
			return n, err
		}
		logx.Debugf("[store] 明文迁移已加密服务器字段: id=%s password=%v keyContent=%v", r.id, pwChanged, kcChanged)
		n++
	}

	crows, err := db.Query(`SELECT id, password, key_content FROM credentials`)
	if err != nil {
		logx.Errorf("[store] 明文迁移查询凭证失败: err=%v", err)
		return n, err
	}
	var creds []row
	for crows.Next() {
		var r row
		if err := crows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			crows.Close()
			logx.Errorf("[store] 明文迁移扫描凭证行失败: err=%v", err)
			return n, err
		}
		creds = append(creds, r)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		logx.Errorf("[store] 明文迁移遍历凭证失败: err=%v", err)
		return n, err
	}
	logx.Debugf("[store] 明文迁移待检查凭证: count=%d", len(creds))
	for _, r := range creds {
		pw, kc := r.password, r.keyContent
		pwChanged, kcChanged := false, false
		if pw != "" && !cryptox.IsEncrypted(pw) {
			enc, err := cipher.EncryptField(pw)
			if err != nil {
				logx.Errorf("[store] 明文迁移加密凭证密码失败: id=%s err=%v", r.id, err)
				return n, err
			}
			pw = enc
			pwChanged = true
		}
		if kc != "" && !cryptox.IsEncrypted(kc) {
			enc, err := cipher.EncryptField(kc)
			if err != nil {
				logx.Errorf("[store] 明文迁移加密凭证私钥失败: id=%s err=%v", r.id, err)
				return n, err
			}
			kc = enc
			kcChanged = true
		}
		if !pwChanged && !kcChanged {
			continue
		}
		if _, err := db.Exec(`UPDATE credentials SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			logx.Errorf("[store] 明文迁移写回凭证敏感字段失败: id=%s err=%v", r.id, err)
			return n, err
		}
		logx.Debugf("[store] 明文迁移已加密凭证字段: id=%s password=%v keyContent=%v", r.id, pwChanged, kcChanged)
		n++
	}
	if n == 0 {
		logx.Infof("[store] 明文敏感字段迁移完成: 无需迁移 servers=%d credentials=%d elapsed=%s",
			len(servers), len(creds), time.Since(start))
	} else {
		logx.Infof("[store] 明文敏感字段迁移完成: migrated=%d servers=%d credentials=%d elapsed=%s",
			n, len(servers), len(creds), time.Since(start))
	}
	return n, nil
}

// ReencryptAllSecrets 在主密钥轮换时用 oldKey→newKey 重加密全部敏感字段。
func ReencryptAllSecrets(db *sql.DB, oldKey, newKey []byte) error {
	if db == nil || len(oldKey) == 0 || len(newKey) == 0 {
		logx.Errorf("[store] 重加密敏感字段参数无效: db=%v oldKeyLen=%d newKeyLen=%d",
			db != nil, len(oldKey), len(newKey))
		return fmt.Errorf("%s", i18n.T("重加密参数无效"))
	}
	start := time.Now()
	logx.Infof("[store] 开始重加密全部敏感字段")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, password, key_content FROM servers`)
	if err != nil {
		logx.Errorf("[store] 重加密查询服务器失败: err=%v", err)
		return err
	}
	type row struct {
		id, password, keyContent string
	}
	var servers []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			rows.Close()
			logx.Errorf("[store] 重加密扫描服务器行失败: err=%v", err)
			return err
		}
		servers = append(servers, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	logx.Debugf("[store] 重加密待处理服务器: count=%d", len(servers))
	for _, r := range servers {
		pw, err := cryptox.Reencrypt(oldKey, newKey, r.password)
		if err != nil {
			logx.Errorf("[store] 重加密服务器密码失败: id=%s err=%v", r.id, err)
			return err
		}
		kc, err := cryptox.Reencrypt(oldKey, newKey, r.keyContent)
		if err != nil {
			logx.Errorf("[store] 重加密服务器私钥失败: id=%s err=%v", r.id, err)
			return err
		}
		if _, err := tx.Exec(`UPDATE servers SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			logx.Errorf("[store] 重加密写回服务器敏感字段失败: id=%s err=%v", r.id, err)
			return err
		}
		logx.Debugf("[store] 重加密服务器完成: id=%s", r.id)
	}

	crows, err := tx.Query(`SELECT id, password, key_content FROM credentials`)
	if err != nil {
		logx.Errorf("[store] 重加密查询凭证失败: err=%v", err)
		return err
	}
	var creds []row
	for crows.Next() {
		var r row
		if err := crows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			crows.Close()
			logx.Errorf("[store] 重加密扫描凭证行失败: err=%v", err)
			return err
		}
		creds = append(creds, r)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return err
	}
	logx.Debugf("[store] 重加密待处理凭证: count=%d", len(creds))
	for _, r := range creds {
		pw, err := cryptox.Reencrypt(oldKey, newKey, r.password)
		if err != nil {
			logx.Errorf("[store] 重加密凭证密码失败: id=%s err=%v", r.id, err)
			return err
		}
		kc, err := cryptox.Reencrypt(oldKey, newKey, r.keyContent)
		if err != nil {
			logx.Errorf("[store] 重加密凭证私钥失败: id=%s err=%v", r.id, err)
			return err
		}
		if _, err := tx.Exec(`UPDATE credentials SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			logx.Errorf("[store] 重加密写回凭证敏感字段失败: id=%s err=%v", r.id, err)
			return err
		}
		logx.Debugf("[store] 重加密凭证完成: id=%s", r.id)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logx.Infof("[store] 重加密全部敏感字段完成: servers=%d credentials=%d elapsed=%s",
		len(servers), len(creds), time.Since(start))
	return nil
}
