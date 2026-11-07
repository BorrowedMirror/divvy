//go:build e2e

// Package e2e drives the real RKL app end to end: a test-hook build of cmd/rkl, the fake game
// window from tools/fake-game and a mock trade2 API, all on the current desktop session.
//
//	go test -tags e2e ./e2e/...
//
// The tests need a display (DISPLAY, WAYLAND_DISPLAY, or the Windows/macOS desktop); the
// scripts in e2e/scripts provide one on Linux. Everything is controlled through the app's
// test hook (internal/app/testhook.go): `state`, `settings`, `save_settings`, `tray`, `quit`,
// and `dom <window> <selector>` for read-only checks of the rendered web views.
//
// Environment:
//
//	RKL_E2E_APP          test-hook app binary (default target/go/rkl-e2e)
//	RKL_E2E_FAKE_GAME    fake game binary (default target/e2e/debug/fake-game)
//	RKL_E2E_TRIGGER      how a price check is started: hook (`trigger` over the hook), xdotool
//	                     (a real XTest key press of the global hotkey), signal (SIGUSR2/SIGUSR1),
//	                     or auto (default: xdotool on X11, hook elsewhere)
//	RKL_E2E_FOCUS_CMD    `sh -c` command that focuses the fake game window before a trigger
//	                     (default on X11: xdotool windowactivate)
//	RKL_E2E_HOTKEY, RKL_E2E_HOTKEY_PINNED
//	                     xdotool chords of the hotkeys (default ctrl+d, ctrl+alt+d)
//	RKL_E2E_ARTIFACTS    directory that keeps the app and fake-game logs (default: a temp dir,
//	                     whose logs are printed when a test fails)
//	RKL_E2E_TIMEOUT_SECS how long to wait for a price (default 30)
//	RKL_TEST_HOOK        fixed hook port (default: a free port per app start)
package e2e
