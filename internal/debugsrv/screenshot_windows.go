//go:build windows

package debugsrv

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"time"
	"unsafe"
)

// 通过 GDI 抓取应用窗口（标题固定为 ding-ssh）的位图：CDP 在 Wails v2.13 下不可用，
// 截图能力改由此提供，用于 AI 目视校验界面（例如"横幅顶部是否完整"）。
var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	gdi32                      = syscall.NewLazyDLL("gdi32.dll")
	procFindWindowW            = user32.NewProc("FindWindowW")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procIsIconic               = user32.NewProc("IsIconic")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procPrintWindow            = user32.NewProc("PrintWindow")
)

const (
	srcCopy      = 0x00CC0020
	dibRGBColors = 0
	biRGB        = 0
	// PW_RENDERFULLCONTENT：让 PrintWindow 渲染完整内容（含 DirectComposition/GPU 合成的 WebView2）
	pwRenderFullContent = 0x00000002
)

type rect struct{ Left, Top, Right, Bottom int32 }

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// captureWindowPNG 抓取窗口当前显示内容并编码为 PNG。
// 说明：走桌面 DC 的 BitBlt，窗口被完全遮挡或最小化时会抓到遮挡内容/空白。
func captureWindowPNG() ([]byte, error) {
	title, err := syscall.UTF16PtrFromString("ding-ssh")
	if err != nil {
		return nil, err
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return nil, fmt.Errorf("未找到窗口（标题应为 ding-ssh）")
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return nil, fmt.Errorf("窗口已最小化，无法截图")
	}
	var r rect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return nil, fmt.Errorf("GetWindowRect 失败")
	}
	w := int(r.Right - r.Left)
	h := int(r.Bottom - r.Top)
	if w <= 0 || h <= 0 || w > 10000 || h > 10000 {
		return nil, fmt.Errorf("窗口尺寸异常: %dx%d", w, h)
	}

	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer func() { _, _, _ = procReleaseDC.Call(0, hdcScreen) }()
	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer func() { _, _, _ = procDeleteDC.Call(hdcMem) }()
	hbm, _, _ := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(w), uintptr(h))
	if hbm == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer func() { _, _, _ = procDeleteObject.Call(hbm) }()
	if old, _, _ := procSelectObject.Call(hdcMem, hbm); old != 0 {
		defer func() { _, _, _ = procSelectObject.Call(hdcMem, old) }()
	}

	// 首选 PrintWindow(PW_RENDERFULLCONTENT)：直接让窗口自己渲染，不依赖是否在前台，
	// 也不会抓到遮挡它的其他窗口。失败时才回退到桌面 BitBlt（需要窗口可见且尽量置前）。
	if ret, _, _ := procPrintWindow.Call(hwnd, hdcMem, pwRenderFullContent); ret == 0 {
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		time.Sleep(320 * time.Millisecond)
		if ret, _, _ := procBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h), hdcScreen,
			uintptr(r.Left), uintptr(r.Top), srcCopy); ret == 0 {
			return nil, fmt.Errorf("PrintWindow 与 BitBlt 均失败")
		}
	}

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // 负数 = 自上而下
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB
	buf := make([]byte, w*h*4)
	if ret, _, _ := procGetDIBits.Call(hdcMem, hbm, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors); ret == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = buf[i*4+2] // BGRX → RGBA
		img.Pix[i*4+1] = buf[i*4+1]
		img.Pix[i*4+2] = buf[i*4+0]
		img.Pix[i*4+3] = 255
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
