package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
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
	"time"

	"ding-ssh/internal/logx"
)

type Plan struct {
	Source, Target string
	PID            int
	Restart        bool
	SourceSHA256   string `json:",omitempty"`
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
	if e = checkWritable(p); e != nil {
		if runtime.GOOS == "windows" && errors.Is(e, os.ErrPermission) {
			logx.Infof("[updater] 安装目录需要管理员权限，安装时将请求 UAC 授权: dir=%q", filepath.Dir(p))
			return p, nil
		}
		logx.Warnf("[updater] 安装目录不可写，无法自动更新: dir=%q err=%v", filepath.Dir(p), e)
		return "", fmt.Errorf("安装目录不可写，请将应用安装到当前用户可写目录后更新: %w", e)
	}
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
	p := Plan{Source: source, Target: target, PID: os.Getpid(), Restart: restart}
	if runtime.GOOS == "windows" {
		p.SourceSHA256, e = fileSHA256(source)
		if e != nil {
			return fmt.Errorf("校验待安装程序失败: %w", e)
		}
	}
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	plan := helper + ".json"
	if e = os.WriteFile(plan, b, 0600); e != nil {
		logx.Warnf("[updater] 写入安装计划文件失败: path=%q err=%v", plan, e)
		return e
	}
	logx.Debugf("[updater] 安装计划已写入: path=%q", plan)
	planHash := sha256.Sum256(b)
	cmd := exec.Command(helper, "--install-update", plan, hex.EncodeToString(planHash[:]))
	if e := cmd.Start(); e != nil {
		logx.Warnf("[updater] 启动安装助手进程失败: helper=%q err=%v", helper, e)
		return e
	}
	logx.Debugf("[updater] 安装助手进程已启动: helper=%q", helper)
	return nil
}
func Helper() bool {
	if handled, err := elevatedHelper(); handled {
		if err != nil {
			logx.Errorf("[updater] 管理员安装助手失败: err=%v", err)
			os.Exit(1)
		}
		return true
	}
	if len(os.Args) < 2 || os.Args[1] != "--install-update" {
		return false
	}
	if len(os.Args) != 4 || len(os.Args[3]) != sha256.Size*2 {
		logx.Errorf("[updater] 安装助手参数无效")
		return true
	}
	logx.Infof("[updater] 安装助手启动: plan=%q", os.Args[2])
	p, b, e := readInstallPlan(os.Args[2], os.Args[3])
	if e != nil {
		logx.Errorf("[updater] 安装助手读取计划文件失败: path=%q err=%v", os.Args[2], e)
		reportInstallError(e)
		return true
	}
	logx.Debugf("[updater] 安装计划: source=%q target=%q pid=%d restart=%v", p.Source, p.Target, p.PID, p.Restart)
	// The helper is started only during normal shutdown; never replace a running application.
	waitStart := time.Now()
	if e = waitForParent(p.PID); e != nil {
		reportInstallError(e)
		return true
	}
	logx.Debugf("[updater] 主进程已退出，开始替换文件: waited=%s", time.Since(waitStart))
	e = completeUpdate(p, func() error {
		return installWithPermissions(p, os.Args[2], b)
	}, func() error {
		if runtime.GOOS == "darwin" {
			return exec.Command("open", p.Target).Start()
		}
		return exec.Command(p.Target).Start()
	})
	if e != nil {
		reportInstallError(e)
	}
	return true
}
