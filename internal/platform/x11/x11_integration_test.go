//go:build linux && x11test

package x11

// End-to-end tests against a real X server: a private Xvfb (+ openbox when installed), or the
// display in DIVVY_X11TEST_DISPLAY. Run with:
//
//	go test -tags x11test ./internal/platform/x11/
//
// They need xdotool and xclip; the copy test also needs the fake game
// (DIVVY_FAKE_GAME, default target/e2e/fake-game at the repository root).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

func need(t *testing.T, bins ...string) {
	t.Helper()
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s not installed", b)
		}
	}
}

func waitUntil(t *testing.T, what string, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// xserver starts Xvfb (and openbox) on a free display, or uses DIVVY_X11TEST_DISPLAY.
func xserver(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("DIVVY_X11TEST_DISPLAY"); d != "" {
		return d
	}
	need(t, "Xvfb")
	n := 300 + os.Getpid()%2000
	for ; ; n++ {
		_, e1 := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n))
		_, e2 := os.Stat(fmt.Sprintf("/tmp/.X%d-lock", n))
		if errors.Is(e1, os.ErrNotExist) && errors.Is(e2, os.ErrNotExist) {
			break
		}
	}
	display := fmt.Sprintf(":%d", n)
	xvfb := exec.Command("Xvfb", display, "-screen", "0", "1280x800x24", "-nolisten", "tcp")
	if err := xvfb.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = xvfb.Process.Kill(); _ = xvfb.Wait() })
	waitUntil(t, "Xvfb", 10*time.Second, func() bool {
		c, err := xgb.NewConnDisplay(display)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
	if _, err := exec.LookPath("openbox"); err == nil {
		wm := exec.Command("openbox", "--sm-disable")
		wm.Env = xenv(display)
		if err := wm.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = wm.Process.Kill(); _ = wm.Wait() })
		x, err := connect(display, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer x.close()
		check := internAtom(t, x.c, "_NET_SUPPORTING_WM_CHECK")
		waitUntil(t, "openbox", 10*time.Second, func() bool {
			return len(x.property32(x.root, check, xproto.AtomWindow, 1)) > 0
		})
		// openbox publishes the check window slightly before it selects SubstructureRedirect.
		time.Sleep(time.Second)
	} else {
		t.Log("openbox not installed: running without a window manager")
	}
	return display
}

func xenv(display string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "DISPLAY=") || strings.HasPrefix(kv, "WAYLAND_DISPLAY=")
	})
	return append(env, "DISPLAY="+display)
}

func internAtom(t *testing.T, c *xgb.Conn, name string) xproto.Atom {
	t.Helper()
	r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil {
		t.Fatal(err)
	}
	return r.Atom
}

func run(t *testing.T, display string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = xenv(display)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return strings.TrimSpace(string(out))
}

func setClipboard(t *testing.T, display, text string) {
	t.Helper()
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Env = xenv(display)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func clipboard(display string) string {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o")
	cmd.Env = xenv(display)
	out, _ := cmd.Output()
	return string(out)
}

// fakeGame starts the fake game and waits for READY; lines gets its later stdout lines.
func fakeGame(t *testing.T, display string) (cmd *exec.Cmd, lines <-chan string, item string) {
	t.Helper()
	root := repoRoot(t)
	bin := os.Getenv("DIVVY_FAKE_GAME")
	if bin == "" {
		bin = filepath.Join(root, "target/e2e/fake-game")
		if _, err := os.Stat(bin); err != nil {
			// A worktree shares the main checkout's build output.
			bin = filepath.Join(root, "../../target/e2e/fake-game")
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("fake game not built: %v", err)
	}
	corpus := filepath.Join(root, "internal/core/testdata/corpus")
	b, err := os.ReadFile(filepath.Join(corpus, "currency/chaos-orb.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "--corpus-id", "currency/chaos-orb", "--corpus-dir", corpus, "--exit-after", "120", "--size", "640x480")
	cmd.Env = xenv(display)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ch := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	select {
	case l := <-ch:
		if l != "READY" {
			t.Fatalf("fake game said %q", l)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("fake game never READY")
	}
	return cmd, ch, string(b)
}

func nextWindowEvent(t *testing.T, ch <-chan platform.WindowEvent, what string, pred func(platform.WindowEvent) bool) platform.WindowEvent {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("tracker channel closed waiting for %s", what)
			}
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event", what)
		}
	}
}

func fakeKey(t *testing.T, x *xConn, keycode uint8, down bool) {
	t.Helper()
	typ := byte(evKeyRelease)
	if down {
		typ = evKeyPress
	}
	if err := xtest.FakeInputChecked(x.c, typ, keycode, 0, x.root, 0, 0, 0).Check(); err != nil {
		t.Fatal(err)
	}
	_, _ = xproto.GetInputFocus(x.c).Reply()
}

func keyIsDown(t *testing.T, x *xConn, keycode uint8) bool {
	t.Helper()
	r, err := xproto.QueryKeymap(x.c).Reply()
	if err != nil {
		t.Fatal(err)
	}
	k := keymap(r.Keys[:32])
	return isDown(&k, keycode)
}

func TestX11EndToEnd(t *testing.T) {
	need(t, "xdotool", "xclip")
	display := xserver(t)
	parts, err := New(Options{Display: display})
	if err != nil {
		t.Fatal(err)
	}
	c := parts.Capabilities
	if parts.Kind != platform.BackendX11 || !c.WindowTracking || !c.OverlayAlwaysOnTop || !c.OverlayClickThrough || !c.PassiveHook || !c.KeystrokeInjection {
		t.Fatalf("capabilities %+v", c)
	}
	tracker := parts.WindowTracker.(*tracker)
	t.Cleanup(tracker.close)
	x, err := connect(display, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(x.close)
	if err := x.useExtension("XTEST"); err != nil {
		t.Fatal(err)
	}

	if _, ok := tracker.GameWindow(); ok {
		t.Fatal("no game yet")
	}
	if tracker.IsGameFocused() {
		t.Fatal("no game to focus")
	}
	events, err := tracker.Subscribe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Subscribe(t.Context()); !errors.Is(err, platform.ErrAlreadyStarted) {
		t.Fatalf("second Subscribe: %v", err)
	}

	game, gameOut, item := fakeGame(t, display)
	found := nextWindowEvent(t, events, "Found", func(e platform.WindowEvent) bool { return e.Type == platform.WindowFound })
	if found.Info.Title != platform.GameWindowTitle || found.Info.Rect.Width != 640 || found.Info.Rect.Height != 480 {
		t.Fatalf("found %+v", found.Info)
	}
	gameID := strconv.FormatUint(found.Info.NativeID, 10)
	t.Logf("tracker found the fake game: %+v", found.Info)

	t.Run("TrackerFollowsMovesAndFocus", func(t *testing.T) {
		run(t, display, "xdotool", "windowmove", gameID, "300", "200")
		nextWindowEvent(t, events, "Moved", func(e platform.WindowEvent) bool {
			return e.Type == platform.WindowMoved && e.Rect.X != found.Info.Rect.X
		})
		waitUntil(t, "game rect to settle", 5*time.Second, func() bool {
			w, ok := tracker.GameWindow()
			return ok && w.Rect.X >= 250 && w.Rect.Y >= 150
		})
		run(t, display, "xdotool", "windowactivate", "--sync", gameID)
		waitUntil(t, "game focused", 5*time.Second, tracker.IsGameFocused)
		w, _ := tracker.GameWindow()
		if !w.Focused {
			t.Fatal("GameWindow().Focused")
		}
		if id, ok, err := activeWindow(display); err != nil || (ok && id != found.Info.NativeID) {
			t.Fatalf("ActiveWindow = %d %v %v", id, ok, err)
		}
	})

	t.Run("RecordHookSeesXdotoolKeys", func(t *testing.T) {
		hookCtx, stopHook := context.WithCancel(t.Context())
		input, err := parts.InputListener.Start(hookCtx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parts.InputListener.Start(t.Context()); !errors.Is(err, platform.ErrAlreadyStarted) {
			t.Fatalf("second Start: %v", err)
		}
		run(t, display, "xdotool", "key", "F5")
		var seen []platform.InputEvent
		deadline := time.After(5 * time.Second)
		for !slices.Contains(seen, platform.InputEvent{Type: platform.KeyUp, Key: "F5"}) {
			select {
			case ev := <-input:
				seen = append(seen, ev)
			case <-deadline:
				t.Fatalf("hook saw %v", seen)
			}
		}
		if !slices.Contains(seen, platform.InputEvent{Type: platform.KeyDown, Key: "F5"}) {
			t.Fatalf("no KeyDown F5 in %v", seen)
		}
		// Keyboard only: the wheel is not recorded.
		run(t, display, "xdotool", "click", "5")
		select {
		case ev := <-input:
			t.Fatalf("keyboard-only hook saw %v", ev)
		case <-time.After(200 * time.Millisecond):
		}
		// Stopping frees the process-wide hook for a mouse-recording one.
		stopHook()
		waitUntil(t, "hook closed", 5*time.Second, func() bool {
			select {
			case _, ok := <-input:
				return !ok
			default:
				return false
			}
		})
		input, err = newListener(display, true).Start(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		run(t, display, "xdotool", "click", "5")
		deadline = time.After(5 * time.Second)
		for {
			select {
			case ev := <-input:
				if ev == (platform.InputEvent{Type: platform.Wheel, Delta: -1}) {
					return
				}
			case <-deadline:
				t.Fatal("mouse hook never saw the wheel")
			}
		}
	})

	t.Run("XTestChordReachesGame", func(t *testing.T) {
		run(t, display, "xdotool", "windowactivate", "--sync", gameID)
		waitUntil(t, "game focused", 5*time.Second, tracker.IsGameFocused)
		setClipboard(t, display, "previous")
		if err := parts.InputSimulator.SendChord(platform.CopyChord(false)); err != nil {
			t.Fatal(err)
		}
		select {
		case l := <-gameOut:
			if l != "COPIED 1" {
				t.Fatalf("game said %q", l)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("game never saw Ctrl+C")
		}
		waitUntil(t, "item on the clipboard", 5*time.Second, func() bool { return clipboard(display) == item })
		if err := parts.InputSimulator.SendChord(platform.NewChord("NoSuchKey")); !errors.Is(err, platform.ErrInvalidChord) {
			t.Fatalf("invalid chord: %v", err)
		}
	})

	t.Run("RespectsHeldKeys", func(t *testing.T) {
		const kcCtrl, kcShift, kcA = 37, 50, 38
		ctrlF5 := platform.NewChord("F5", platform.Ctrl)
		fakeKey(t, x, kcCtrl, true)
		fakeKey(t, x, kcShift, true)
		if err := parts.InputSimulator.SendChord(ctrlF5); err != nil {
			t.Fatal(err)
		}
		if !keyIsDown(t, x, kcCtrl) {
			t.Fatal("held Ctrl must not be synthetically released")
		}
		if keyIsDown(t, x, kcShift) {
			t.Fatal("extra held Shift must be released first")
		}
		fakeKey(t, x, kcCtrl, false)

		// Hotkey key held: SendChord waits for its release.
		fakeKey(t, x, kcA, true)
		go func() {
			time.Sleep(150 * time.Millisecond)
			c, err := connect(display, nil)
			if err != nil {
				return
			}
			defer c.close()
			_ = c.useExtension("XTEST")
			_ = xtest.FakeInputChecked(c.c, evKeyRelease, kcA, 0, c.root, 0, 0, 0).Check()
		}()
		start := time.Now()
		if err := parts.InputSimulator.SendChord(ctrlF5); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited < 120*time.Millisecond || waited > 2*time.Second {
			t.Fatalf("did not wait for the release: %v", waited)
		}
		// Never released: capped (~400ms), then sent anyway.
		fakeKey(t, x, kcA, true)
		start = time.Now()
		if err := parts.InputSimulator.SendChord(ctrlF5); err != nil {
			t.Fatal(err)
		}
		waited := time.Since(start)
		fakeKey(t, x, kcA, false)
		if waited < 350*time.Millisecond || waited > 2*time.Second {
			t.Fatalf("cap: %v", waited)
		}
	})

	t.Run("StylesOverlay", func(t *testing.T) {
		win, err := xproto.NewWindowId(x.c)
		if err != nil {
			t.Fatal(err)
		}
		if err := xproto.CreateWindowChecked(x.c, 0, win, x.root, 0, 0, 640, 480, 0, xproto.WindowClassInputOutput, 0, 0, nil).Check(); err != nil {
			t.Fatal(err)
		}
		ov, err := newOverlay(display, uint32(win))
		if err != nil {
			t.Fatal(err)
		}
		defer ov.close()
		if err := ov.Prepare(); err != nil {
			t.Fatal(err)
		}
		if err := ov.ShowAt(platform.Rect{X: 10, Y: 20, Width: 300, Height: 200}); err != nil {
			t.Fatal(err)
		}
		above := internAtom(t, x.c, "_NET_WM_STATE_ABOVE")
		state := internAtom(t, x.c, "_NET_WM_STATE")
		waitUntil(t, "_NET_WM_STATE_ABOVE", 5*time.Second, func() bool {
			return slices.Contains(x.property32(win, state, xproto.AtomAtom, 32), uint32(above))
		})
		if hints := x.property32(win, xproto.AtomWmHints, xproto.AtomWmHints, 9); len(hints) < 2 || hints[0]&1 == 0 || hints[1] != 0 {
			t.Fatalf("WM_HINTS input: %v", hints)
		}
		if err := x.useExtension("SHAPE"); err != nil {
			t.Fatal(err)
		}
		rects := func(kind shape.Kind) []xproto.Rectangle {
			r, err := shape.GetRectangles(x.c, win, kind).Reply()
			if err != nil {
				t.Fatal(err)
			}
			return r.Rectangles
		}
		if err := ov.SetClickThrough(true); err != nil {
			t.Fatal(err)
		}
		if len(rects(shape.SkInput)) != 0 {
			t.Fatal("click-through => empty input shape")
		}
		if err := ov.SetClickThrough(false); err != nil {
			t.Fatal(err)
		}
		if len(rects(shape.SkInput)) == 0 {
			t.Fatal("input shape restored")
		}

		owner := found.Info.NativeID
		if err := ov.RaiseAbove(owner); err != nil {
			t.Fatal(err)
		}
		if tf := x.property32(win, xproto.AtomWmTransientFor, xproto.AtomWindow, 1); len(tf) != 1 || uint64(tf[0]) != owner {
			t.Fatalf("WM_TRANSIENT_FOR = %v", tf)
		}
		if err := ov.RaiseAbove(0); err != nil {
			t.Fatal(err)
		}
		if err := ov.Activate(); err != nil {
			t.Fatal(err)
		}
		if hints := x.property32(win, xproto.AtomWmHints, xproto.AtomWmHints, 9); len(hints) < 2 || hints[1] != 1 {
			t.Fatalf("Activate must allow input: %v", hints)
		}

		if err := ov.SetRoundedShape(100, 100, 10); err != nil {
			t.Fatal(err)
		}
		b := rects(shape.SkBounding)
		if covered(b, 0, 0) || !covered(b, 50, 50) || !covered(b, 10, 0) {
			t.Fatalf("rounded bounding shape %v", b)
		}
		if err := ov.SetRoundedShape(0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if ext, err := shape.QueryExtents(x.c, win).Reply(); err != nil || ext.BoundingShaped {
			t.Fatalf("radius 0 removes the shape: %v %v", ext, err)
		}

		if err := ov.MoveDirect(123, 77); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "direct move", 5*time.Second, func() bool {
			r, ok := x.rect(win)
			// A reparenting WM places its frame there; the client sits inside the decorations.
			return ok && r.X >= 123 && r.X < 160 && r.Y >= 77 && r.Y < 140
		})
		if err := ov.Hide(); err != nil {
			t.Fatal(err)
		}

		// Give focus back to the game (refocus_game).
		if err := parts.ActivateWindow(owner); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "game refocused", 5*time.Second, tracker.IsGameFocused)

		run(t, display, "xdotool", "mousemove", "321", "234")
		waitUntil(t, "cursor", 5*time.Second, func() bool {
			cx, cy, ok := parts.Cursor()
			return ok && cx == 321 && cy == 234
		})

		if _, err := overlayFromNative(display, nil); !errors.Is(err, platform.ErrUnsupported) {
			t.Fatalf("nil native window: %v", err)
		}
	})

	// Closing the game -> Lost.
	_ = game.Process.Kill()
	nextWindowEvent(t, events, "Lost", func(e platform.WindowEvent) bool { return e.Type == platform.WindowLost })
	if _, ok := tracker.GameWindow(); ok {
		t.Fatal("game is gone")
	}
}

// Test-only helpers.

// close releases the overlay's connection.
func (o *overlay) close() { o.conn.close() }

// close closes the connection GameWindow and IsGameFocused use.
func (t *tracker) close() { t.conn.close() }

// activeWindow is the window the WM reports as active (_NET_ACTIVE_WINDOW), if any.
func activeWindow(display string) (uint64, bool, error) {
	var id uint64
	err := withShared(display, func(x *xConn) error {
		if v := x.property32(x.root, x.atoms.netActiveWindow, xproto.AtomWindow, 1); len(v) > 0 {
			id = uint64(v[0])
		}
		return nil
	})
	return id, err == nil && id != 0, err
}
