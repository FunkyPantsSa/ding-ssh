package updater

import (
	"crypto/sha256"
	"ding-ssh/internal/i18n"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
)

func checkWritable(target string) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".ding-update-check-")
	if err != nil {
		return err
	}
	name := f.Name()
	return errors.Join(f.Close(), os.Remove(name))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readInstallPlan(path, expectedHash string) (Plan, []byte, error) {
	var p Plan
	b, err := os.ReadFile(path)
	if err != nil {
		return p, nil, err
	}
	if expectedHash != "" {
		h := sha256.Sum256(b)
		if !strings.EqualFold(hex.EncodeToString(h[:]), expectedHash) {
			return p, nil, fmt.Errorf("%s", i18n.T("安装计划校验失败，更新已取消"))
		}
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return p, nil, fmt.Errorf(i18n.T("解析安装计划失败: %w"), err)
	}
	if !filepath.IsAbs(p.Source) || !filepath.IsAbs(p.Target) || p.PID <= 0 || filepath.Clean(p.Source) == filepath.Clean(p.Target) {
		return p, nil, fmt.Errorf("%s", i18n.T("安装计划路径或进程无效"))
	}
	return p, b, nil
}

// Stage on the destination filesystem and verify the actual bytes being installed
// before moving the old executable. This also handles downloads on another drive.
func stageUpdate(p Plan) (staged string, err error) {
	info, err := os.Stat(p.Source)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("%s", i18n.T("更新程序不是普通文件"))
		}
		staged, err = os.MkdirTemp(filepath.Dir(p.Target), ".ding-update-stage-")
		if err != nil {
			return "", err
		}
		if err = exec.Command("/usr/bin/ditto", p.Source, staged).Run(); err != nil {
			os.RemoveAll(staged)
			return "", err
		}
		if err = os.Chmod(staged, info.Mode().Perm()); err != nil {
			os.RemoveAll(staged)
			return "", err
		}
		return staged, nil
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s", i18n.T("更新程序不是普通文件"))
	}
	in, err := os.Open(p.Source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(p.Target), ".ding-update-stage-")
	if err != nil {
		return "", err
	}
	staged = out.Name()
	defer func() {
		if err != nil {
			os.Remove(staged)
		}
	}()
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	err = errors.Join(copyErr, out.Chmod(0755), out.Sync(), out.Close())
	if err != nil {
		return staged, err
	}
	if p.SourceSHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), p.SourceSHA256) {
		return staged, fmt.Errorf("%s", i18n.T("待安装程序校验失败，原版本未修改"))
	}
	return staged, nil
}

func applyPlan(p Plan) error {
	return applyPlanWithRename(p, os.Rename)
}

func installWithElevation(p Plan, planPath string, planBytes []byte, writable func(string) error, elevated bool, elevate func(string, string) error) error {
	err := writable(p.Target)
	if err == nil {
		err = applyPlan(p)
		if err == nil {
			return nil
		}
	}
	if !errors.Is(err, os.ErrPermission) || elevated {
		return err
	}
	if len(p.SourceSHA256) != sha256.Size*2 {
		return fmt.Errorf("%s", i18n.T("待安装程序缺少校验信息，更新已取消"))
	}
	h := sha256.Sum256(planBytes)
	logx.Infof("[updater] 请求管理员权限安装更新: target=%q", p.Target)
	return elevate(planPath, hex.EncodeToString(h[:]))
}

func applyPlanWithRename(p Plan, rename func(string, string) error) error {
	backup := p.Target + ".update-backup"
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf(i18n.T("已存在未清理的备份文件: %s"), backup)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged, err := stageUpdate(p)
	if err != nil {
		return fmt.Errorf(i18n.T("准备替换程序失败: %w"), err)
	}
	defer os.RemoveAll(staged)
	if err = rename(p.Target, backup); err != nil {
		return fmt.Errorf(i18n.T("备份旧版本失败: %w"), err)
	}
	if err = rename(staged, p.Target); err != nil {
		if rollbackErr := rename(backup, p.Target); rollbackErr != nil {
			return fmt.Errorf(i18n.T("替换失败: %w；恢复旧版本失败（备份位于 %s）: %v"), err, backup, rollbackErr)
		}
		return fmt.Errorf(i18n.T("替换失败，已恢复旧版本: %w"), err)
	}
	if err = os.RemoveAll(backup); err != nil {
		logx.Warnf("[updater] 更新已安装，清理旧版本备份失败: backup=%q err=%v", backup, err)
	}
	logx.Infof("[updater] 新版本已替换: target=%q", p.Target)
	return nil
}

// Only the original, unelevated helper restarts the application, including the
// old version when UAC is cancelled. The elevated worker only replaces files.
func completeUpdate(p Plan, install, restart func() error) error {
	err := install()
	if p.Restart {
		err = errors.Join(err, restart())
	}
	return err
}

func reportInstallError(err error) {
	logx.Errorf("[updater] 安装更新失败: err=%v", err)
	if writeErr := os.WriteFile(filepath.Join(appdir.ConfigDir(), "update-error.log"), []byte(err.Error()), 0600); writeErr != nil {
		logx.Warnf("[updater] 写入更新错误记录失败: err=%v", writeErr)
	}
	showInstallError(err)
}
