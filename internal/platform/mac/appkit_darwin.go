package mac

// AppKit (Objective-C) helpers for the overlay, over purego's objc runtime bindings.

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	pathFoundation = "/System/Library/Frameworks/Foundation.framework/Foundation"
	pathAppKit     = "/System/Library/Frameworks/AppKit.framework/AppKit"
)

var loadAppKit = sync.OnceValue(func() error {
	for _, fw := range []string{pathFoundation, pathAppKit} {
		if _, err := purego.Dlopen(fw, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return fmt.Errorf("load %s: %w", fw, err)
		}
	}
	return nil
})

// sel resolves a selector. sel_registerName is idempotent and cheap (a lookup in the runtime's
// own selector table), so nothing is cached here.
func sel(name string) objc.SEL { return objc.RegisterName(name) }

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// runOnMain runs fn on the main thread and waits for it. Like Wails, it schedules fn on the main
// run loop in the common modes (not the main dispatch queue), so it also runs inside modal and
// menu-tracking loops.
func runOnMain(fn func()) {
	if objc.Send[bool](class("NSThread"), sel("isMainThread")) {
		fn()
		return
	}
	done := make(chan struct{})
	block := objc.NewBlock(func(objc.Block) {
		defer close(done)
		fn()
	})
	main := cfRunLoopGetMain()
	cfRunLoopPerformBlock(main, kCFRunLoopCommonModes, block)
	cfRunLoopWakeUp(main)
	// CFRunLoopPerformBlock copies the block; drop our reference.
	block.Release()
	<-done
}
