package store

import (
	"database/sql"
	"fmt"

	"ding-ssh/internal/cryptox"
)

// MigratePlaintextSecrets 将 servers/credentials 中的明文 password/key_content 加密落库。
func MigratePlaintextSecrets(db *sql.DB, cipher cryptox.FieldCipher) (int, error) {
	if db == nil || cipher == nil {
		return 0, nil
	}
	n := 0
	rows, err := db.Query(`SELECT id, password, key_content FROM servers`)
	if err != nil {
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
			return n, err
		}
		servers = append(servers, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return n, err
	}
	for _, r := range servers {
		pw, kc := r.password, r.keyContent
		changed := false
		if pw != "" && !cryptox.IsEncrypted(pw) {
			enc, err := cipher.EncryptField(pw)
			if err != nil {
				return n, err
			}
			pw = enc
			changed = true
		}
		if kc != "" && !cryptox.IsEncrypted(kc) {
			enc, err := cipher.EncryptField(kc)
			if err != nil {
				return n, err
			}
			kc = enc
			changed = true
		}
		if !changed {
			continue
		}
		if _, err := db.Exec(`UPDATE servers SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			return n, err
		}
		n++
	}

	crows, err := db.Query(`SELECT id, password, key_content FROM credentials`)
	if err != nil {
		return n, err
	}
	var creds []row
	for crows.Next() {
		var r row
		if err := crows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			crows.Close()
			return n, err
		}
		creds = append(creds, r)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return n, err
	}
	for _, r := range creds {
		pw, kc := r.password, r.keyContent
		changed := false
		if pw != "" && !cryptox.IsEncrypted(pw) {
			enc, err := cipher.EncryptField(pw)
			if err != nil {
				return n, err
			}
			pw = enc
			changed = true
		}
		if kc != "" && !cryptox.IsEncrypted(kc) {
			enc, err := cipher.EncryptField(kc)
			if err != nil {
				return n, err
			}
			kc = enc
			changed = true
		}
		if !changed {
			continue
		}
		if _, err := db.Exec(`UPDATE credentials SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ReencryptAllSecrets 在主密钥轮换时用 oldKey→newKey 重加密全部敏感字段。
func ReencryptAllSecrets(db *sql.DB, oldKey, newKey []byte) error {
	if db == nil || len(oldKey) == 0 || len(newKey) == 0 {
		return fmt.Errorf("重加密参数无效")
	}
	rows, err := db.Query(`SELECT id, password, key_content FROM servers`)
	if err != nil {
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
			return err
		}
		servers = append(servers, r)
	}
	rows.Close()
	for _, r := range servers {
		pw, err := cryptox.Reencrypt(oldKey, newKey, r.password)
		if err != nil {
			return err
		}
		kc, err := cryptox.Reencrypt(oldKey, newKey, r.keyContent)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE servers SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			return err
		}
	}

	crows, err := db.Query(`SELECT id, password, key_content FROM credentials`)
	if err != nil {
		return err
	}
	var creds []row
	for crows.Next() {
		var r row
		if err := crows.Scan(&r.id, &r.password, &r.keyContent); err != nil {
			crows.Close()
			return err
		}
		creds = append(creds, r)
	}
	crows.Close()
	for _, r := range creds {
		pw, err := cryptox.Reencrypt(oldKey, newKey, r.password)
		if err != nil {
			return err
		}
		kc, err := cryptox.Reencrypt(oldKey, newKey, r.keyContent)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE credentials SET password = ?, key_content = ? WHERE id = ?`, pw, kc, r.id); err != nil {
			return err
		}
	}
	return nil
}
