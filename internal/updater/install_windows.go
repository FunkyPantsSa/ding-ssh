package updater

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const elevatedInstallArg = "--install-update-elevated"

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// SHELLEXECUTEINFOW; uintptr-sized fields preserve the native Windows alignment.
type shellExecuteInfo struct {
	Size, Mask                  uint32
	Window                      windows.HWND
	Verb, File, Parameters, Dir *uint16
	Show                        int32
	Instance, IDList            uintptr
	Class                       *uint16
	ClassKey                    windows.Handle
	HotKey                      uint32
	Icon                        windows.Handle
	Process                     windows.Handle
}

func waitForParent(pid int) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil // The parent has already exited.
	}
	if err != nil {
		return fmt.Errorf("检查主进程退出状态失败: %w", err)
	}
	defer windows.CloseHandle(h)
	status, err := windows.WaitForSingleObject(h, 300000)
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("等待主进程退出超时，更新未安装")
	}
	return nil
}

func installWithPermissions(p Plan, planPath string, planBytes []byte) error {
	return installWithElevation(p, planPath, planBytes, checkWritable, windows.GetCurrentProcessToken().IsElevated(), runElevated)
}

func runElevated(planPath, planHash string) error {
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(helper)
	if err != nil {
		return err
	}
	args := []string{elevatedInstallArg, planPath, planHash}
	for i := range args {
		args[i] = windows.EscapeArg(args[i])
	}
	params, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(helper))
	if err != nil {
		return err
	}
	// ShellExecuteEx may use COM. Keep initialization and the call on one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, syscall.Errno(1)) { // S_FALSE also requires CoUninitialize.
		return fmt.Errorf("初始化管理员授权失败: %w", err)
	}
	defer windows.CoUninitialize()
	info := shellExecuteInfo{
		Mask: 0x40 | 0x100 | 0x400, // NOCLOSEPROCESS | NOASYNC | FLAG_NO_UI
		Verb: verb, File: file, Parameters: params, Dir: dir, Show: windows.SW_HIDE,
	}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(info)
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return fmt.Errorf("已取消管理员授权，更新未安装，原版本保持不变: %w", callErr)
		}
		return fmt.Errorf("启动管理员安装助手失败: %w", callErr)
	}
	if info.Process == 0 {
		return fmt.Errorf("无法获取管理员安装助手进程")
	}
	defer windows.CloseHandle(info.Process)
	if _, err = windows.WaitForSingleObject(info.Process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err = windows.GetExitCodeProcess(info.Process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("管理员安装更新失败（退出码 %d），请检查安装目录或安全软件限制", code)
	}
	return nil
}

func elevatedHelper() (bool, error) {
	if len(os.Args) < 2 || os.Args[1] != elevatedInstallArg {
		return false, nil
	}
	if len(os.Args) != 4 || !windows.GetCurrentProcessToken().IsElevated() || len(os.Args[3]) != sha256.Size*2 {
		return true, fmt.Errorf("管理员安装参数或权限无效")
	}
	p, _, err := readInstallPlan(os.Args[2], os.Args[3])
	if err != nil {
		return true, err
	}
	if len(p.SourceSHA256) != sha256.Size*2 {
		return true, fmt.Errorf("待安装程序缺少校验信息")
	}
	if err = waitForParent(p.PID); err != nil {
		return true, err
	}
	// Never restart the app with the administrator's token or profile. The
	// unelevated helper waits for this process and performs the restart itself.
	return true, applyPlan(p)
}

func showInstallError(err error) {
	text, convErr := windows.UTF16PtrFromString(err.Error())
	if convErr != nil {
		return
	}
	title, _ := windows.UTF16PtrFromString("ding-ssh 更新未完成 / Update not completed")
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONWARNING|windows.MB_SETFOREGROUND)
}
