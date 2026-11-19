package apphost

import (
	"ding-ssh/internal/logx"
	"ding-ssh/internal/sshx"
)

// GetHostTrust 返回已保存的主机密钥信任记录列表（设置页「主机信任」页签）。
func (a *App) GetHostTrust() ([]sshx.HostTrustRecord, error) {
	records, err := sshx.ListHostTrust()
	if err != nil {
		logx.Errorf("[apphost] 读取主机信任列表失败: err=%v", err)
		return nil, err
	}
	logx.Debugf("[apphost] 读取主机信任列表: count=%d", len(records))
	return records, nil
}

// DeleteHostTrust 删除一条主机密钥信任记录（按文件行号 + 指纹定位）。
func (a *App) DeleteHostTrust(line int, fingerprint string) error {
	logx.Debugf("[apphost] 删除主机信任: line=%d fingerprint=%s", line, fingerprint)
	if err := sshx.DeleteHostTrust(line, fingerprint); err != nil {
		logx.Errorf("[apphost] 删除主机信任失败: line=%d fingerprint=%s err=%v", line, fingerprint, err)
		return err
	}
	logx.Infof("[apphost] 删除主机信任完成: line=%d fingerprint=%s", line, fingerprint)
	return nil
}

// ResolveHostTrust 处理一次主机密钥确认：approve=true 接受并写入 known_hosts，
// replace 表示覆盖已变更的旧指纹；approve=false 表示拒绝。
func (a *App) ResolveHostTrust(id string, approve, replace bool) error {
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 处理主机信任被拒（保险库未解锁）: id=%s approve=%v err=%v", id, approve, err)
		return err
	}
	logx.Infof("[apphost] 处理主机密钥信任: id=%s approve=%v replace=%v", id, approve, replace)
	if err := sshx.ResolveHostTrust(id, approve, replace); err != nil {
		logx.Errorf("[apphost] 处理主机密钥信任失败: id=%s approve=%v replace=%v err=%v", id, approve, replace, err)
		return err
	}
	if approve {
		logx.Infof("[apphost] 主机密钥已接受: id=%s replace=%v", id, replace)
	} else {
		logx.Infof("[apphost] 主机密钥已拒绝: id=%s", id)
	}
	return nil
}
