package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ding-ssh/internal/logx"
)

type Plan struct {
	Source, Target string
	PID            int
	Restart        bool
}

func copyFile(src, dst string, mode os.FileMode) error {
	start := time.Now()
	logx.Debugf("[updater] 复制文件: src=%q dst=%q mode=%#o", src, dst, mode)
	in, e := os.Open(src)
	if e != nil {
		logx.Warnf("[updater] 复制文件打开源文件失败: src=%q err=%v", src, e)
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		logx.Warnf("[updater] 复制文件创建目标文件失败: dst=%q err=%v", dst, e)
		return e
	}
	n, e := io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		logx.Warnf("[updater] 复制文件写入失败: src=%q dst=%q copied=%d err=%v", src, dst, n, e)
		return e
	}
	if ce != nil {
		logx.Warnf("[updater] 复制文件关闭目标文件失败: dst=%q err=%v", dst, ce)
	}
	logx.Debugf("[updater] 复制文件完成: src=%q dst=%q bytes=%d elapsed=%s", src, dst, n, time.Since(start))
	return ce
}
func Target() (string, error) {
	logx.Debugf("[updater] 解析更新目标路径")
	p, e := os.Executable()
	if e != nil {
		logx.Warnf("[updater] 获取当前可执行文件路径失败: err=%v", e)
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		logx.Warnf("[updater] 解析可执行文件符号链接失败: path=%q err=%v", p, e)
		return "", e
	}
	if runtime.GOOS == "darwin" {
		i := strings.LastIndex(p, ".app/Contents/MacOS/")
		if i < 0 {
			logx.Warnf("[updater] 当前不在已安装的 .app 中运行，无法自动更新: path=%q", p)
			return "", fmt.Errorf("请从已安装的 .app 中更新")
		}
		p = p[:i+4]
	}
	if runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "" {
		p = os.Getenv("APPIMAGE")
		logx.Debugf("[updater] 检测到 AppImage 运行模式，目标改为 APPIMAGE: path=%q", p)
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".ding-update-check-")
	if e != nil {
		logx.Warnf("[updater] 安装目录不可写，无法自动更新: dir=%q err=%v", filepath.Dir(p), e)
		return "", fmt.Errorf("安装目录不可写，请将应用安装到当前用户可写目录后更新: %w", e)
	}
	f.Close()
	os.Remove(f.Name())
	logx.Debugf("[updater] 更新目标路径已解析: target=%q writable=true", p)
	return p, nil
}
func Prepare(file string) (string, error) {
	start := time.Now()
	dir := filepath.Join(filepath.Dir(file), "unpacked")
	logx.Debugf("[updater] 解压更新包: file=%q dir=%q", file, dir)
	if e := os.Mkdir(dir, 0700); e != nil {
		logx.Warnf("[updater] 创建解压目录失败: dir=%q err=%v", dir, e)
		return "", e
	}
	if strings.HasSuffix(file, ".AppImage") {
		dst := filepath.Join(dir, "ding-ssh")
		logx.Debugf("[updater] 更新包为 AppImage，直接复制: src=%q", file)
		err := copyFile(file, dst, 0755)
		if err != nil {
			logx.Warnf("[updater] 复制 AppImage 更新包失败: src=%q err=%v", file, err)
		}
		return dst, err
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("/usr/bin/ditto", "-x", "-k", file, dir)
		if b, e := cmd.CombinedOutput(); e != nil {
			logx.Warnf("[updater] 解压 macOS 更新包失败: file=%q output=%q err=%v", file, string(b), e)
			return "", fmt.Errorf("解压失败: %s: %w", b, e)
		}
		p := filepath.Join(dir, "ding-ssh.app")
		_, e := os.Stat(filepath.Join(p, "Contents/MacOS/ding-ssh"))
		if e != nil {
			logx.Warnf("[updater] 更新包缺少主程序: path=%q err=%v", p, e)
		} else {
			logx.Debugf("[updater] 更新包解压完成: app=%q elapsed=%s", p, time.Since(start))
		}
		return p, e
	}
	if strings.HasSuffix(file, ".zip") {
		logx.Debugf("[updater] 按 zip 格式解压更新包: file=%q", file)
		z, e := zip.OpenReader(file)
		if e != nil {
			logx.Warnf("[updater] 打开 zip 更新包失败: file=%q err=%v", file, e)
			return "", e
		}
		defer z.Close()
		for _, f := range z.File {
			if f.Name == "ding-ssh.exe" {
				in, e := f.Open()
				if e != nil {
					logx.Warnf("[updater] 打开 zip 内主程序失败: entry=%q err=%v", f.Name, e)
					return "", e
				}
				defer in.Close()
				dst := filepath.Join(dir, f.Name)
				out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
				if e != nil {
					logx.Warnf("[updater] 创建解压目标文件失败: path=%q err=%v", dst, e)
					return "", e
				}
				_, e = io.Copy(out, io.LimitReader(in, 2<<30))
				out.Close()
				if e != nil {
					logx.Warnf("[updater] 解压 zip 主程序失败: path=%q err=%v", dst, e)
				} else {
					logx.Debugf("[updater] 解压 zip 主程序完成: path=%q elapsed=%s", dst, time.Since(start))
				}
				return dst, e
			}
		}
		logx.Warnf("[updater] zip 更新包中缺少 ding-ssh.exe: file=%q", file)
	} else {
		logx.Debugf("[updater] 按 tar.gz 格式解压更新包: file=%q", file)
		f, e := os.Open(file)
		if e != nil {
			logx.Warnf("[updater] 打开 tar.gz 更新包失败: file=%q err=%v", file, e)
			return "", e
		}
		defer f.Close()
		gz, e := gzip.NewReader(f)
		if e != nil {
			logx.Warnf("[updater] 解压 gzip 失败: file=%q err=%v", file, e)
			return "", e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e != nil {
				break
			}
			if h.Name == "ding-ssh" && h.Typeflag == tar.TypeReg {
				dst := filepath.Join(dir, "ding-ssh")
				out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
				if e != nil {
					logx.Warnf("[updater] 创建解压目标文件失败: path=%q err=%v", dst, e)
					return "", e
				}
				_, e = io.Copy(out, io.LimitReader(tr, 2<<30))
				out.Close()
				if e != nil {
					logx.Warnf("[updater] 解压 tar 主程序失败: path=%q err=%v", dst, e)
				} else {
					logx.Debugf("[updater] 解压 tar 主程序完成: path=%q elapsed=%s", dst, time.Since(start))
				}
				return dst, e
			}
		}
		logx.Warnf("[updater] tar.gz 更新包中缺少 ding-ssh: file=%q", file)
	}
	logx.Warnf("[updater] 更新包中缺少可执行程序: file=%q", file)
	return "", fmt.Errorf("更新包中缺少程序")
}
func Launch(source, target string, restart bool) error {
	logx.Infof("[updater] 启动安装助手: source=%q target=%q restart=%v pid=%d", source, target, restart, os.Getpid())
	exe, e := os.Executable()
	if e != nil {
		logx.Warnf("[updater] 获取当前可执行文件路径失败: err=%v", e)
		return e
	}
	helper := filepath.Join(filepath.Dir(filepath.Dir(source)), "installer")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if e = copyFile(exe, helper, 0700); e != nil {
		logx.Warnf("[updater] 复制安装助手失败: helper=%q err=%v", helper, e)
		return e
	}
	p := Plan{source, target, os.Getpid(), restart}
	b, _ := json.Marshal(p)
	plan := helper + ".json"
	if e = os.WriteFile(plan, b, 0600); e != nil {
		logx.Warnf("[updater] 写入安装计划文件失败: path=%q err=%v", plan, e)
		return e
	}
	logx.Debugf("[updater] 安装计划已写入: path=%q", plan)
	cmd := exec.Command(helper, "--install-update", plan)
	if e := cmd.Start(); e != nil {
		logx.Warnf("[updater] 启动安装助手进程失败: helper=%q err=%v", helper, e)
		return e
	}
	logx.Debugf("[updater] 安装助手进程已启动: helper=%q", helper)
	return nil
}
func Helper() bool {
	if len(os.Args) != 3 || os.Args[1] != "--install-update" {
		return false
	}
	logx.Infof("[updater] 安装助手启动: plan=%q", os.Args[2])
	b, e := os.ReadFile(os.Args[2])
	if e != nil {
		logx.Errorf("[updater] 安装助手读取计划文件失败: path=%q err=%v", os.Args[2], e)
		return true
	}
	var p Plan
	if json.Unmarshal(b, &p) != nil {
		logx.Errorf("[updater] 安装助手解析计划文件失败: path=%q", os.Args[2])
		return true
	}
	logx.Debugf("[updater] 安装计划: source=%q target=%q pid=%d restart=%v", p.Source, p.Target, p.PID, p.Restart)
	// The helper is started only during normal shutdown; never replace a running application.
	exited := false
	waitStart := time.Now()
	for i := 0; i < 300; i++ {
		var alive bool
		if runtime.GOOS == "windows" {
			b, _ := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(p.PID), "/NH").Output()
			alive = strings.Contains(string(b), strconv.Itoa(p.PID))
		} else {
			alive = exec.Command("kill", "-0", strconv.Itoa(p.PID)).Run() == nil
		}
		if !alive {
			exited = true
			break
		}
		if i > 0 && i%30 == 0 {
			logx.Debugf("[updater] 安装助手等待主进程退出: pid=%d waited=%s attempt=%d", p.PID, time.Since(waitStart), i)
		}
		time.Sleep(time.Second)
	}
	if !exited {
		logx.Errorf("[updater] 安装中止: 等待主进程退出超时 pid=%d waited=%s", p.PID, time.Since(waitStart))
		return true
	}
	logx.Debugf("[updater] 主进程已退出，开始替换文件: waited=%s", time.Since(waitStart))
	backup := p.Target + ".update-backup"
	if _, e = os.Stat(backup); e == nil {
		logx.Errorf("[updater] 安装中止: 已存在未清理的备份文件 backup=%q", backup)
		return true
	}
	if e = os.Rename(p.Target, backup); e != nil {
		logx.Errorf("[updater] 安装中止: 备份旧版本失败 target=%q backup=%q err=%v", p.Target, backup, e)
		return true
	}
	logx.Debugf("[updater] 旧版本已备份: backup=%q", backup)
	if e = os.Rename(p.Source, p.Target); e != nil {
		// Downloads may reside on another filesystem; copy to the destination before renaming.
		logx.Debugf("[updater] 跨文件系统移动失败，改为复制后替换: err=%v", e)
		staged := p.Target + ".update-new"
		if runtime.GOOS == "darwin" {
			e = exec.Command("/usr/bin/ditto", p.Source, staged).Run()
		} else {
			e = copyFile(p.Source, staged, 0755)
		}
		if e == nil {
			e = os.Rename(staged, p.Target)
		}
	}
	if e != nil {
		logx.Errorf("[updater] 替换新版本失败，回滚旧版本: target=%q err=%v", p.Target, e)
		os.Rename(backup, p.Target)
		os.WriteFile(p.Target+".update-error", []byte(e.Error()), 0600)
		return true
	}
	os.RemoveAll(backup)
	logx.Infof("[updater] 新版本已替换: target=%q restart=%v elapsed=%s", p.Target, p.Restart, time.Since(waitStart))
	if p.Restart {
		var restartErr error
		if runtime.GOOS == "darwin" {
			restartErr = exec.Command("open", p.Target).Start()
		} else {
			restartErr = exec.Command(p.Target).Start()
		}
		if restartErr != nil {
			logx.Warnf("[updater] 重启新版本失败: target=%q err=%v", p.Target, restartErr)
		} else {
			logx.Infof("[updater] 已重启新版本: target=%q", p.Target)
		}
	}
	return true
}
