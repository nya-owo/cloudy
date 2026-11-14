package app

import (
	"image"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	gdi32                    = syscall.NewLazyDLL("gdi32.dll")
	dwmapi                   = syscall.NewLazyDLL("dwmapi.dll")
	procGetAsyncKeyState     = user32.NewProc("GetAsyncKeyState")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procIsWindow             = user32.NewProc("IsWindow")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procGetForegroundWnd     = user32.NewProc("GetForegroundWindow")
	procWindowFromPoint      = user32.NewProc("WindowFromPoint")
	procGetAncestor          = user32.NewProc("GetAncestor")
	procGetClassNameW        = user32.NewProc("GetClassNameW")
	procMonitorFromPointTray = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection     = gdi32.NewProc("CreateDIBSection")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procBitBlt               = gdi32.NewProc("BitBlt")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procDwmGetWindowAttrTray = dwmapi.NewProc("DwmGetWindowAttribute")
)

type trayPoint struct{ x, y int32 }
type trayRect struct{ left, top, right, bottom int32 }

func cursorPos() trayPoint {
	var pt trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt
}

// trayHostAtCursor finds the window of the icon just right-clicked: the
// taskbar (Shell_TrayWnd, Shell_SecondaryTrayWnd), or the overflow of hidden
// icons (TopLevelWindowForOverflowXamlIsland on Windows 11,
// NotifyIconOverflowWindow on 10), which folds as soon as another window is
// activated. overflow is everything not the taskbar itself.
func trayHostAtCursor() (host uintptr, overflow bool) {
	pt := cursorPos()
	w, _, _ := procWindowFromPoint.Call(uintptr(*(*int64)(unsafe.Pointer(&pt))))
	if w == 0 {
		return 0, false
	}
	root, _, _ := procGetAncestor.Call(w, 2) // GA_ROOT
	var buf [64]uint16
	procGetClassNameW.Call(root, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	switch syscall.UTF16ToString(buf[:]) {
	case "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return root, false
	}
	return root, true
}

// hostGone reports whether the overflow the menu opened from has folded:
// hidden, destroyed, or cloaked by DWM, as Windows 11 folds it.
func hostGone(host uintptr) bool {
	if ok, _, _ := procIsWindow.Call(host); ok == 0 {
		return true
	}
	if v, _, _ := procIsWindowVisible.Call(host); v == 0 {
		return true
	}
	var cloaked uint32
	if r, _, _ := procDwmGetWindowAttrTray.Call(host, 14 /* DWMWA_CLOAKED */, uintptr(unsafe.Pointer(&cloaked)), 4); r == 0 && cloaked != 0 {
		return true
	}
	return false
}

// watchTrayPopup closes the tray menu at hwnd as a menu closes: on a press
// outside it, when the foreground leaves it after it had it, and when the
// overflow it opened from (host, 0 for the taskbar) folds. The flyout closes
// itself when deactivated, but Windows does not always activate it, and one
// opened from the overflow is not focusable, so that the overflow stays
// open beside it. done stops the watch; dismiss runs on its goroutine.
func watchTrayPopup(hwnd, host uintptr, done <-chan struct{}, dismiss func()) {
	if hwnd == 0 {
		return
	}
	go func() {
		t := time.NewTicker(40 * time.Millisecond)
		defer t.Stop()
		down := func(vk uintptr) bool {
			r, _, _ := procGetAsyncKeyState.Call(vk)
			return r&0x8000 != 0
		}
		// A button still held from the click that opened the menu is not
		// a press outside it.
		held := down(0x01) || down(0x02) || down(0x04)
		hadForeground := false
		for {
			select {
			case <-done:
				return
			case <-t.C:
			}
			if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
				continue // not shown yet, or hiding
			}
			if host != 0 && hostGone(host) {
				dismiss()
				return
			}
			fg, _, _ := procGetForegroundWnd.Call()
			if fg == hwnd {
				hadForeground = true
			} else if hadForeground {
				dismiss()
				return
			}
			pressed := down(0x01) || down(0x02) || down(0x04)
			if !pressed {
				held = false
				continue
			}
			if held {
				continue
			}
			pt := cursorPos()
			var r trayRect
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			if pt.x < r.left || pt.x >= r.right || pt.y < r.top || pt.y >= r.bottom {
				dismiss()
				return
			}
			held = true // inside: the menu handles it
		}
	}()
}

// captureBehind copies what the monitor under the pointer shows, before the
// menu covers it, for the menu's glass to blur as the app's glass blurs the
// page under it. crop then cuts out what is behind the menu's window, once
// it is placed, and frees the copy; nil where the copy failed.
func captureBehind() (crop func(hwnd uintptr) image.Image) {
	pt := cursorPos()
	mon, _, _ := procMonitorFromPointTray.Call(uintptr(*(*int64)(unsafe.Pointer(&pt))), 2 /* nearest */)
	var mi struct {
		size          uint32
		monitor, work trayRect
		flags         uint32
	}
	mi.size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return nil
	}
	m := mi.monitor
	w, h := m.right-m.left, m.bottom-m.top
	if w <= 0 || h <= 0 {
		return nil
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil
	}
	defer procReleaseDC.Call(0, screen)
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	if dc == 0 {
		return nil
	}
	var bi struct {
		size                 uint32
		width, height        int32
		planes, bitCount     uint16
		compression, sizeImg uint32
		xppm, yppm           int32
		clrUsed, clrImp      uint32
	}
	bi.size = uint32(unsafe.Sizeof(bi))
	bi.width, bi.height, bi.planes, bi.bitCount = w, -h, 1, 32 // top-down BGRA
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		procDeleteDC.Call(dc)
		return nil
	}
	old, _, _ := procSelectObject.Call(dc, bmp)
	free := func() {
		procSelectObject.Call(dc, old)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(dc)
	}
	// CAPTUREBLT takes layered windows too, as the overflow of hidden icons.
	if r, _, _ := procBitBlt.Call(dc, 0, 0, uintptr(w), uintptr(h), screen, uintptr(m.left), uintptr(m.top), 0x00CC0020|0x40000000); r == 0 {
		free()
		return nil
	}
	pix := unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	return func(hwnd uintptr) image.Image {
		defer free()
		var r trayRect
		if hwnd == 0 {
			return nil
		}
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		out := image.NewRGBA(image.Rect(0, 0, int(r.right-r.left), int(r.bottom-r.top)))
		for y := r.top; y < r.bottom; y++ {
			sy := y - m.top
			if sy < 0 || sy >= h {
				continue
			}
			for x := r.left; x < r.right; x++ {
				sx := x - m.left
				if sx < 0 || sx >= w {
					continue
				}
				s := (int(sy)*int(w) + int(sx)) * 4
				d := out.PixOffset(int(x-r.left), int(y-r.top))
				out.Pix[d], out.Pix[d+1], out.Pix[d+2], out.Pix[d+3] = pix[s+2], pix[s+1], pix[s], 255
			}
		}
		return out
	}
}
