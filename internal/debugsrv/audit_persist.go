package debugsrv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Persistent audit deliberately contains metadata only. Arbitrary command,
// terminal output, error strings and caller-provided argument bodies stay out
// of this file, even when an argument does not have a recognizable secret key.
const auditFileLimit = 2 << 20

func (a *AuditLog) ConfigurePersistence(path string) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if path == "" {
		a.persistencePath = ""
		return nil
	}
	if !filepath.IsAbs(path) {
		return errors.New("审计路径必须是绝对路径")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := auditRegularFile(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	a.persistencePath = path
	return nil
}

func auditRegularFile(path string) error {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("审计文件必须是普通文件，禁止符号链接")
	}
	return nil
}

func (a *AuditLog) persistLocked(rec AuditRecord) error {
	if _, known := ToolCapabilities(rec.Tool); !known {
		switch rec.Tool {
		case "authorization.settings", "authorization.approve", "authorization.issue", "authorization.revoke":
		default:
			rec.Tool = "unknown"
		}
	}
	if _, known := KnownCapability(rec.Cap); !known {
		rec.Cap = ""
	}
	switch rec.Source {
	case "mcp", "http", "ui", "authorization":
	default:
		rec.Source = "unknown"
	}
	path := a.persistencePath
	if err := auditRegularFile(path); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil && st.Size() >= auditFileLimit {
		if err := auditRegularFile(path + ".1"); err != nil {
			return err
		}
		if err := os.Remove(path + ".1"); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	// Fixed metadata schema: no args, error text, output or authentication data.
	entry := struct {
		ID          string `json:"id"`
		TS          int64  `json:"ts"`
		ClientID    string `json:"clientId"`
		OperationID string `json:"operationId,omitempty"`
		Source      string `json:"source"`
		Tool        string `json:"tool"`
		Cap         string `json:"cap"`
		OK          bool   `json:"ok"`
		DurationMS  int64  `json:"durationMs"`
	}{rec.ID, rec.TS, rec.ClientID, rec.OperationID, rec.Source, rec.Tool, rec.Cap, rec.OK, rec.DurationMS}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
