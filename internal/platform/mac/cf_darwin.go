package mac

// cgo-free bindings to CoreFoundation, CoreGraphics and ApplicationServices (HIServices) over
// purego, loaded once on first use. Wails' darwin purego backend loads frameworks the same way.
// AppKit lives in appkit_darwin.go.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	pathCoreFoundation      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	pathCoreGraphics        = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	pathApplicationServices = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
)

// CoreFoundation constants.
const (
	cfNumberSInt32Type          = 3
	cfNumberSInt64Type          = 4
	cfNumberFloat64Type         = 6
	cfStringEncodingUTF8 uint32 = 0x08000100
	cfRunLoopRunFinished int32  = 1
)

// cgPoint is CGPoint (CGFloat is float64 on 64-bit macOS).
type cgPoint struct{ X, Y float64 }

// cgRect is CGRect / NSRect.
type cgRect struct {
	Origin cgPoint
	Size   struct{ Width, Height float64 }
}

var (
	// CoreFoundation.
	cfRelease                         func(cf uintptr)
	cfArrayGetCount                   func(array uintptr) int
	cfArrayGetValueAtIndex            func(array uintptr, i int) uintptr
	cfDictionaryGetValue              func(dict, key uintptr) uintptr
	cfDictionaryCreate                func(alloc uintptr, keys, values *uintptr, n int, keyCallbacks, valueCallbacks uintptr) uintptr
	cfGetTypeID                       func(cf uintptr) uint
	cfNumberGetTypeID                 func() uint
	cfStringGetTypeID                 func() uint
	cfDictionaryGetTypeID             func() uint
	cfNumberGetValue                  func(number uintptr, typ int, out unsafe.Pointer) bool
	cfStringCreateWithCString         func(alloc uintptr, s string, encoding uint32) uintptr
	cfStringGetLength                 func(s uintptr) int
	cfStringGetMaximumSizeForEncoding func(length int, encoding uint32) int
	cfStringGetCString                func(s uintptr, buf *byte, size int, encoding uint32) bool
	cfRunLoopGetCurrent               func() uintptr
	cfRunLoopGetMain                  func() uintptr
	cfRunLoopAddSource                func(rl, source, mode uintptr)
	cfRunLoopRemoveSource             func(rl, source, mode uintptr)
	cfRunLoopRunInMode                func(mode uintptr, seconds float64, returnAfterSourceHandled bool) int32
	cfRunLoopStop                     func(rl uintptr)
	cfRunLoopPerformBlock             func(rl, mode uintptr, block objc.Block)
	cfRunLoopWakeUp                   func(rl uintptr)
	cfMachPortCreateRunLoopSource     func(alloc, port uintptr, order int) uintptr
	cfMachPortInvalidate              func(port uintptr)

	kCFRunLoopDefaultMode           uintptr
	kCFRunLoopCommonModes           uintptr
	kCFBooleanTrue                  uintptr
	kCFTypeDictionaryKeyCallBacks   uintptr // address of the struct
	kCFTypeDictionaryValueCallBacks uintptr // address of the struct

	// CoreGraphics.
	cgWindowListCopyWindowInfo  func(option, relativeToWindow uint32) uintptr
	cgEventTapCreate            func(tap, place, options uint32, mask uint64, callback, userInfo uintptr) uintptr
	cgEventTapEnable            func(tap uintptr, enable bool)
	cgEventGetIntegerValueField func(event uintptr, field uint32) int64
	cgEventSetIntegerValueField func(event uintptr, field uint32, value int64)
	cgEventGetFlags             func(event uintptr) uint64
	cgEventSetFlags             func(event uintptr, flags uint64)
	cgEventGetLocation          func(event uintptr) cgPoint
	cgEventCreate               func(source uintptr) uintptr
	cgEventSourceCreate         func(state int32) uintptr
	cgEventSourceFlagsState     func(state int32) uint64
	cgEventCreateKeyboardEvent  func(source uintptr, keycode uint16, keyDown bool) uintptr
	cgEventPost                 func(tap uint32, event uintptr)
	// TCC checks (macOS 10.15+); nil when missing, which means "not needed".
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool
	cgPreflightListenEventAccess   func() bool
	cgRequestListenEventAccess     func() bool

	kCGWindowBounds    uintptr
	kCGWindowNumber    uintptr
	kCGWindowOwnerPID  uintptr
	kCGWindowLayer     uintptr
	kCGWindowOwnerName uintptr
	kCGWindowName      uintptr
	cfStrX, cfStrY     uintptr
	cfStrWidth         uintptr
	cfStrHeight        uintptr

	// ApplicationServices (HIServices).
	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool
	axObserverCreate              func(pid int32, callback uintptr, out *uintptr) int32
	axUIElementCreateApplication  func(pid int32) uintptr
	axObserverAddNotification     func(observer, element, notification, refcon uintptr) int32
	axObserverGetRunLoopSource    func(observer uintptr) uintptr

	kAXTrustedCheckOptionPrompt uintptr
)

var loadSys = sync.OnceValue(func() error {
	cf, err := purego.Dlopen(pathCoreFoundation, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("load CoreFoundation: %w", err)
	}
	cg, err := purego.Dlopen(pathCoreGraphics, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("load CoreGraphics: %w", err)
	}
	as, err := purego.Dlopen(pathApplicationServices, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("load ApplicationServices: %w", err)
	}
	var errs []error
	fn := func(ptr any, lib uintptr, name string) {
		if _, err := purego.Dlsym(lib, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		purego.RegisterLibFunc(ptr, lib, name)
	}
	optional := func(ptr any, lib uintptr, name string) {
		if _, err := purego.Dlsym(lib, name); err == nil {
			purego.RegisterLibFunc(ptr, lib, name)
		}
	}
	// addr is the address of an exported variable; value reads the pointer stored there.
	addr := func(lib uintptr, name string) uintptr {
		a, err := purego.Dlsym(lib, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		return a
	}
	value := func(lib uintptr, name string) uintptr {
		a := addr(lib, name)
		if a == 0 {
			return 0
		}
		return **(**uintptr)(unsafe.Pointer(&a))
	}

	fn(&cfRelease, cf, "CFRelease")
	fn(&cfArrayGetCount, cf, "CFArrayGetCount")
	fn(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
	fn(&cfDictionaryGetValue, cf, "CFDictionaryGetValue")
	fn(&cfDictionaryCreate, cf, "CFDictionaryCreate")
	fn(&cfGetTypeID, cf, "CFGetTypeID")
	fn(&cfNumberGetTypeID, cf, "CFNumberGetTypeID")
	fn(&cfStringGetTypeID, cf, "CFStringGetTypeID")
	fn(&cfDictionaryGetTypeID, cf, "CFDictionaryGetTypeID")
	fn(&cfNumberGetValue, cf, "CFNumberGetValue")
	fn(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	fn(&cfStringGetLength, cf, "CFStringGetLength")
	fn(&cfStringGetMaximumSizeForEncoding, cf, "CFStringGetMaximumSizeForEncoding")
	fn(&cfStringGetCString, cf, "CFStringGetCString")
	fn(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
	fn(&cfRunLoopGetMain, cf, "CFRunLoopGetMain")
	fn(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
	fn(&cfRunLoopRemoveSource, cf, "CFRunLoopRemoveSource")
	fn(&cfRunLoopRunInMode, cf, "CFRunLoopRunInMode")
	fn(&cfRunLoopStop, cf, "CFRunLoopStop")
	fn(&cfRunLoopPerformBlock, cf, "CFRunLoopPerformBlock")
	fn(&cfRunLoopWakeUp, cf, "CFRunLoopWakeUp")
	fn(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
	fn(&cfMachPortInvalidate, cf, "CFMachPortInvalidate")
	kCFRunLoopDefaultMode = value(cf, "kCFRunLoopDefaultMode")
	kCFRunLoopCommonModes = value(cf, "kCFRunLoopCommonModes")
	kCFBooleanTrue = value(cf, "kCFBooleanTrue")
	kCFTypeDictionaryKeyCallBacks = addr(cf, "kCFTypeDictionaryKeyCallBacks")
	kCFTypeDictionaryValueCallBacks = addr(cf, "kCFTypeDictionaryValueCallBacks")

	fn(&cgWindowListCopyWindowInfo, cg, "CGWindowListCopyWindowInfo")
	fn(&cgEventTapCreate, cg, "CGEventTapCreate")
	fn(&cgEventTapEnable, cg, "CGEventTapEnable")
	fn(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")
	fn(&cgEventSetIntegerValueField, cg, "CGEventSetIntegerValueField")
	fn(&cgEventGetFlags, cg, "CGEventGetFlags")
	fn(&cgEventSetFlags, cg, "CGEventSetFlags")
	fn(&cgEventGetLocation, cg, "CGEventGetLocation")
	fn(&cgEventCreate, cg, "CGEventCreate")
	fn(&cgEventSourceCreate, cg, "CGEventSourceCreate")
	fn(&cgEventSourceFlagsState, cg, "CGEventSourceFlagsState")
	fn(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
	fn(&cgEventPost, cg, "CGEventPost")
	optional(&cgPreflightScreenCaptureAccess, cg, "CGPreflightScreenCaptureAccess")
	optional(&cgRequestScreenCaptureAccess, cg, "CGRequestScreenCaptureAccess")
	optional(&cgPreflightListenEventAccess, cg, "CGPreflightListenEventAccess")
	optional(&cgRequestListenEventAccess, cg, "CGRequestListenEventAccess")
	kCGWindowBounds = value(cg, "kCGWindowBounds")
	kCGWindowNumber = value(cg, "kCGWindowNumber")
	kCGWindowOwnerPID = value(cg, "kCGWindowOwnerPID")
	kCGWindowLayer = value(cg, "kCGWindowLayer")
	kCGWindowOwnerName = value(cg, "kCGWindowOwnerName")
	kCGWindowName = value(cg, "kCGWindowName")

	fn(&axIsProcessTrusted, as, "AXIsProcessTrusted")
	fn(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
	fn(&axObserverCreate, as, "AXObserverCreate")
	fn(&axUIElementCreateApplication, as, "AXUIElementCreateApplication")
	fn(&axObserverAddNotification, as, "AXObserverAddNotification")
	fn(&axObserverGetRunLoopSource, as, "AXObserverGetRunLoopSource")
	kAXTrustedCheckOptionPrompt = value(as, "kAXTrustedCheckOptionPrompt")

	if err := errors.Join(errs...); err != nil {
		return err
	}
	// Constant strings for the window bounds dictionary; never released.
	cfStrX, cfStrY = cfString("X"), cfString("Y")
	cfStrWidth, cfStrHeight = cfString("Width"), cfString("Height")
	return nil
})

// cfString creates a CFString (+1; the caller releases it, or keeps it forever).
func cfString(s string) uintptr {
	return cfStringCreateWithCString(0, s, cfStringEncodingUTF8)
}

// goString copies a CFString; ok is false when v is not a CFString.
func goString(v uintptr) (string, bool) {
	if v == 0 || cfGetTypeID(v) != cfStringGetTypeID() {
		return "", false
	}
	size := cfStringGetMaximumSizeForEncoding(cfStringGetLength(v), cfStringEncodingUTF8) + 1
	buf := make([]byte, size)
	if !cfStringGetCString(v, &buf[0], size, cfStringEncodingUTF8) {
		return "", false
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), true
		}
	}
	return string(buf), true
}

// isNumber reports whether v is a CFNumber.
func isNumber(v uintptr) bool { return v != 0 && cfGetTypeID(v) == cfNumberGetTypeID() }

// numberI64 reads a CFNumber as int64; 0 when v is not a number or the conversion is lossy.
func numberI64(v uintptr) int64 {
	var out int64
	if !isNumber(v) || !cfNumberGetValue(v, cfNumberSInt64Type, unsafe.Pointer(&out)) {
		return 0
	}
	return out
}

// numberI32 reads a CFNumber as int32; 0 when v is not a number or the conversion is lossy.
func numberI32(v uintptr) int32 {
	var out int32
	if !isNumber(v) || !cfNumberGetValue(v, cfNumberSInt32Type, unsafe.Pointer(&out)) {
		return 0
	}
	return out
}

// numberF64 reads a CFNumber as float64; 0 when v is not a number or the conversion is lossy.
func numberF64(v uintptr) float64 {
	var out float64
	if !isNumber(v) || !cfNumberGetValue(v, cfNumberFloat64Type, unsafe.Pointer(&out)) {
		return 0
	}
	return out
}

// stopRunLoopWhenDone stops rl (a run loop of the calling thread) once ctx is done, so a
// CFRunLoopRunInMode in progress returns early. The returned release must be called before
// the thread exits: afterwards rl is never touched again.
func stopRunLoopWhenDone(ctx context.Context, rl uintptr) (release func()) {
	var mu sync.Mutex
	alive := true
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if alive {
			cfRunLoopStop(rl)
		}
	})
	return func() {
		stop()
		mu.Lock()
		alive = false
		mu.Unlock()
	}
}
