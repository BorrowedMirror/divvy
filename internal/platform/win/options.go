package win

// Options configures [New].
type Options struct {
	// HookMouse also installs the low-level mouse hook, for Ctrl + wheel stash tab switching.
	// Off by default: a WH_MOUSE_LL hook adds latency to every mouse move in the game.
	HookMouse bool
}
