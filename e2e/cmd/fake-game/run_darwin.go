//go:build darwin

package main

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	pathAppKit     = "/System/Library/Frameworks/AppKit.framework/AppKit"
	pathFoundation = "/System/Library/Frameworks/Foundation.framework/Foundation"

	nsEventTypeKeyDown = 10
	nsEventTypeKeyUp   = 11

	nsEventModifierFlagShift   = 1 << 17
	nsEventModifierFlagControl = 1 << 18
	nsEventModifierFlagOption  = 1 << 19
	nsEventModifierFlagCommand = 1 << 20

	// kVK_ANSI_C / kVK_ANSI_V: the physical C and V keys.
	kVKC, kVKV = 8, 9

	nsWindowStyleMask                    = 1 | 2 | 4 | 8 // titled, closable, miniaturizable, resizable
	nsBackingStoreBuffered               = 2
	nsApplicationActivationPolicyRegular = 0
)

type cgPoint struct{ X, Y float64 }

type cgSize struct{ Width, Height float64 }

type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

func init() { runtime.LockOSThread() } // AppKit wants the main thread

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func nsString(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	p := objc.Send[*byte](ns, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// macPasteboard is the general pasteboard with NSPasteboardTypeString.
type macPasteboard struct{ typeString objc.ID }

func (p macPasteboard) setText(text string) error {
	pb := class("NSPasteboard").Send(sel("generalPasteboard"))
	pb.Send(sel("clearContents"))
	if !objc.Send[bool](pb, sel("setString:forType:"), nsString(text), p.typeString) {
		return errors.New("NSPasteboard setString:forType: failed")
	}
	return nil
}

func (p macPasteboard) text() (string, error) {
	pb := class("NSPasteboard").Send(sel("generalPasteboard"))
	return goString(pb.Send(sel("stringForType:"), p.typeString)), nil
}

func run(g *game, w, h uint32) error {
	appKit, err := purego.Dlopen(pathAppKit, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("loading AppKit: %w", err)
	}
	foundation, err := purego.Dlopen(pathFoundation, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("loading Foundation: %w", err)
	}
	pbType, err := global(appKit, "NSPasteboardTypeString")
	if err != nil {
		return err
	}
	runMode, err := global(foundation, "NSDefaultRunLoopMode")
	if err != nil {
		return err
	}
	cb := macPasteboard{typeString: pbType}

	pool := class("NSAutoreleasePool").Send(sel("new"))
	defer pool.Send(sel("drain"))
	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
	app.Send(sel("finishLaunching"))

	frame := cgRect{Size: cgSize{float64(w), float64(h)}}
	win := class("NSWindow").Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		frame, uint(nsWindowStyleMask), uint(nsBackingStoreBuffered), false)
	if win == 0 {
		return errors.New("creating the NSWindow failed")
	}
	win.Send(sel("setReleasedWhenClosed:"), false)
	win.Send(sel("setTitle:"), nsString(title))
	paint := func() {
		c := g.color()
		color := class("NSColor").Send(sel("colorWithSRGBRed:green:blue:alpha:"),
			float64(c>>16&0xff)/255, float64(c>>8&0xff)/255, float64(c&0xff)/255, 1.0)
		win.Send(sel("setBackgroundColor:"), color)
		win.Send(sel("display"))
	}
	paint()
	win.Send(sel("center"))
	win.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	app.Send(sel("activateIgnoringOtherApps:"), true)
	g.drawn()

	distantFuture := class("NSDate").Send(sel("distantFuture"))
	for {
		inner := class("NSAutoreleasePool").Send(sel("new"))
		ev := app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"), ^uint(0), distantFuture, runMode, true)
		if ev != 0 {
			switch typ := objc.Send[uint](ev, sel("type")); typ {
			case nsEventTypeKeyDown, nsEventTypeKeyUp:
				if typ == nsEventTypeKeyDown && objc.Send[bool](ev, sel("isARepeat")) {
					break
				}
				flags := objc.Send[uint](ev, sel("modifierFlags"))
				code := objc.Send[uint16](ev, sel("keyCode"))
				chars := goString(ev.Send(sel("charactersIgnoringModifiers")))
				var r rune
				for _, c := range chars {
					r = c
					break
				}
				name := charCode(r)
				if name == "" {
					name = fmt.Sprintf("Unidentified(NativeKeyCode(MacOS(%d)))", code)
				}
				kev := keyEvent{
					pressed: typ == nsEventTypeKeyDown,
					isC:     code == kVKC || r == 'c' || r == 'C',
					isV:     code == kVKV || r == 'v' || r == 'V',
					code:    name,
					mods: mods{
						ctrl:  flags&nsEventModifierFlagControl != 0,
						alt:   flags&nsEventModifierFlagOption != 0,
						shift: flags&nsEventModifierFlagShift != 0,
						super: flags&nsEventModifierFlagCommand != 0,
					},
				}
				if g.key(kev, cb) {
					paint()
				}
			}
			app.Send(sel("sendEvent:"), ev)
			app.Send(sel("updateWindows"))
		}
		inner.Send(sel("drain"))
		if !objc.Send[bool](win, sel("isVisible")) && !objc.Send[bool](win, sel("isMiniaturized")) {
			return nil // closed
		}
	}
}

// global reads an exported Objective-C object variable (an NSString constant).
func global(lib uintptr, name string) (objc.ID, error) {
	a, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return **(**objc.ID)(unsafe.Pointer(&a)), nil
}
