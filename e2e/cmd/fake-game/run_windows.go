//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procAdjustWindowRect = user32.NewProc("AdjustWindowRectEx")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procFillRect         = user32.NewProc("FillRect")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procGetKeyState      = user32.NewProc("GetKeyState")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
)

const (
	wmDestroy    = 0x0002
	wmPaint      = 0x000F
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000
	swShow             = 5
	idcArrow           = 32512
	cfUnicodeText      = 13
	gmemMoveable       = 0x0002

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	// Set-1 scancodes of the physical C and V keys.
	scanC, scanV = 0x2E, 0x2F
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type rect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	hdc         windows.Handle
	erase       int32
	paint       rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

// winGame is the window state; Win32 calls the window procedure on the main (locked) thread.
var winGame struct {
	g    *game
	hwnd windows.HWND
}

//nolint:gochecknoinits // only init runs on the main OS thread, which Win32 windows need
func init() { runtime.LockOSThread() }

func run(g *game, w, h uint32) error {
	winGame.g = g
	inst, err := moduleHandle()
	if err != nil {
		return err
	}
	className, _ := windows.UTF16PtrFromString("RklFakeGame")
	titleW, _ := windows.UTF16PtrFromString(title)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		wndProc:   windows.NewCallback(wndProc),
		instance:  inst,
		cursor:    windows.Handle(cursor),
		className: className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}
	// The requested size is the client area.
	rc := rect{right: int32(w), bottom: int32(h)}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), wsOverlappedWindow, 0, 0)
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(titleW)),
		wsOverlappedWindow, cwUseDefault, cwUseDefault, uintptr(rc.right-rc.left), uintptr(rc.bottom-rc.top),
		0, 0, uintptr(inst), 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	winGame.hwnd = windows.HWND(hwnd)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	var m msg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case 0:
			return nil
		case -1:
			return fmt.Errorf("GetMessageW: %w", err)
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func moduleHandle() (windows.Handle, error) {
	var h windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &h); err != nil {
		return 0, fmt.Errorf("GetModuleHandleEx: %w", err)
	}
	return h, nil
}

// colorRef is a Win32 COLORREF (0x00BBGGRR) for 0xRRGGBB.
func colorRef(rgb uint32) uintptr {
	return uintptr(rgb>>16&0xff | rgb&0xff00 | (rgb&0xff)<<16)
}

func keyDown(vk uintptr) bool {
	r, _, _ := procGetKeyState.Call(vk)
	return r&0x8000 != 0
}

func wndProc(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
	g := winGame.g
	switch message {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		brush, _, _ := procCreateSolidBrush.Call(colorRef(g.color()))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&ps.paint)), brush)
		procDeleteObject.Call(brush)
		procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		g.drawn()
		return 0
	case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
		pressed := message == wmKeyDown || message == wmSysKeyDown
		if pressed && lParam&(1<<30) != 0 { // auto-repeat
			break
		}
		scan := lParam >> 16 & 0xff
		ev := keyEvent{
			pressed: pressed,
			isC:     wParam == 'C' || scan == scanC,
			isV:     wParam == 'V' || scan == scanV,
			code:    vkName(wParam),
			mods: mods{
				ctrl: keyDown(vkControl), alt: keyDown(vkMenu), shift: keyDown(vkShift),
				super: keyDown(vkLWin) || keyDown(vkRWin),
			},
		}
		if g.key(ev, winClipboard{hwnd}) {
			procInvalidateRect.Call(uintptr(hwnd), 0, 0)
		}
		if message == wmKeyDown || message == wmKeyUp {
			return 0
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

// vkName is the W3C KeyboardEvent.code name for a virtual key, as far as the tests care.
func vkName(vk uintptr) string {
	if n := charCode(rune(vk)); n != "" && (vk < 'a' || vk > 'z') {
		return n
	}
	switch vk {
	case 0x0D:
		return "Enter"
	case 0x1B:
		return "Escape"
	case 0x20:
		return "Space"
	case 0x09:
		return "Tab"
	case 0x08:
		return "Backspace"
	}
	return fmt.Sprintf("Unidentified(NativeKeyCode(Windows(%d)))", vk)
}

type winClipboard struct{ hwnd windows.HWND }

// open opens the clipboard, retrying while another process holds it.
func (cb winClipboard) open() error {
	var err error
	for range 20 {
		var r uintptr
		if r, _, err = procOpenClipboard.Call(uintptr(cb.hwnd)); r != 0 {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("OpenClipboard: %w", err)
}

func (cb winClipboard) setText(text string) error {
	u := utf16.Encode([]rune(text))
	u = append(u, 0)
	if err := cb.open(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("EmptyClipboard: %w", err)
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock: %w", err)
	}
	copy(unsafe.Slice(globalPtr(p), len(u)), u)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}

func (cb winClipboard) text() (string, error) {
	if err := cb.open(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", errors.New("no text on the clipboard")
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("GlobalLock: %w", err)
	}
	defer procGlobalUnlock.Call(h)
	return windows.UTF16PtrToString(globalPtr(p)), nil
}

// globalPtr is the memory of a locked HGLOBAL (not Go memory, so the uintptr is stable).
func globalPtr(p uintptr) *uint16 { return *(**uint16)(unsafe.Pointer(&p)) }
