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
)

type Plan struct {
	Source, Target string
	PID            int
	Restart        bool
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
func Target() (string, error) {
	p, e := os.Executable()
	if e != nil {
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	if runtime.GOOS == "darwin" {
		i := strings.LastIndex(p, ".app/Contents/MacOS/")
		if i < 0 {
			return "", fmt.Errorf("请从已安装的 .app 中更新")
		}
		p = p[:i+4]
	}
	if runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "" {
		p = os.Getenv("APPIMAGE")
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".ding-update-check-")
	if e != nil {
		return "", fmt.Errorf("安装目录不可写，请将应用安装到当前用户可写目录后更新: %w", e)
	}
	f.Close()
	os.Remove(f.Name())
	return p, nil
}
func Prepare(file string) (string, error) {
	dir := filepath.Join(filepath.Dir(file), "unpacked")
	if e := os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	if strings.HasSuffix(file, ".AppImage") {
		dst := filepath.Join(dir, "ding-ssh")
		return dst, copyFile(file, dst, 0755)
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("/usr/bin/ditto", "-x", "-k", file, dir)
		if b, e := cmd.CombinedOutput(); e != nil {
			return "", fmt.Errorf("解压失败: %s: %w", b, e)
		}
		p := filepath.Join(dir, "ding-ssh.app")
		_, e := os.Stat(filepath.Join(p, "Contents/MacOS/ding-ssh"))
		return p, e
	}
	if strings.HasSuffix(file, ".zip") {
		z, e := zip.OpenReader(file)
		if e != nil {
			return "", e
		}
		defer z.Close()
		for _, f := range z.File {
			if f.Name == "ding-ssh.exe" {
				in, e := f.Open()
				if e != nil {
					return "", e
				}
				defer in.Close()
				dst := filepath.Join(dir, f.Name)
				out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
				if e != nil {
					return "", e
				}
				_, e = io.Copy(out, io.LimitReader(in, 2<<30))
				out.Close()
				return dst, e
			}
		}
	} else {
		f, e := os.Open(file)
		if e != nil {
			return "", e
		}
		defer f.Close()
		gz, e := gzip.NewReader(f)
		if e != nil {
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
					return "", e
				}
				_, e = io.Copy(out, io.LimitReader(tr, 2<<30))
				out.Close()
				return dst, e
			}
		}
	}
	return "", fmt.Errorf("更新包中缺少程序")
}
func Launch(source, target string, restart bool) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	helper := filepath.Join(filepath.Dir(filepath.Dir(source)), "installer")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if e = copyFile(exe, helper, 0700); e != nil {
		return e
	}
	p := Plan{source, target, os.Getpid(), restart}
	b, _ := json.Marshal(p)
	plan := helper + ".json"
	if e = os.WriteFile(plan, b, 0600); e != nil {
		return e
	}
	cmd := exec.Command(helper, "--install-update", plan)
	return cmd.Start()
}
func Helper() bool {
	if len(os.Args) != 3 || os.Args[1] != "--install-update" {
		return false
	}
	b, e := os.ReadFile(os.Args[2])
	if e != nil {
		return true
	}
	var p Plan
	if json.Unmarshal(b, &p) != nil {
		return true
	}
	// The helper is started only during normal shutdown; never replace a running application.
	exited := false
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
		time.Sleep(time.Second)
	}
	if !exited {
		return true
	}
	backup := p.Target + ".update-backup"
	if _, e = os.Stat(backup); e == nil {
		return true
	}
	if e = os.Rename(p.Target, backup); e != nil {
		return true
	}
	if e = os.Rename(p.Source, p.Target); e != nil {
		// Downloads may reside on another filesystem; copy to the destination before renaming.
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
		os.Rename(backup, p.Target)
		os.WriteFile(p.Target+".update-error", []byte(e.Error()), 0600)
		return true
	}
	os.RemoveAll(backup)
	if p.Restart {
		if runtime.GOOS == "darwin" {
			exec.Command("open", p.Target).Start()
		} else {
			exec.Command(p.Target).Start()
		}
	}
	return true
}
