// Package platformtest provides deterministic in-memory implementations of every
// internal/platform interface, for tests.
//
// Every fake is a pointer handle over shared, mutex-guarded state: put it into a
// [platform.Platform] and keep the same pointer in the test to drive it (Fire, Emit,
// SetWindow) or inspect it (Calls, Sent, History). [New] builds a whole platform of fakes and
// returns the handles:
//
//	p, fakes := platformtest.New(platformtest.Options{ItemText: "Item Class: Stackable Currency\n"})
//	events, _ := p.Hotkeys.Events()
//	fakes.Hotkeys.Fire(platform.Action(platform.PriceCheck))
//	ev := <-events // ev.Action == PriceCheck
package platformtest
