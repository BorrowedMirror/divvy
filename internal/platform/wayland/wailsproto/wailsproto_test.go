//go:build linux && cgo && gtk3 && waylandtest

// Package wailsproto checks that the Wayland overlay works on a real Wails (GTK3) window under
// headless sway: Prepare after Wails created the hidden overlay window and before its first
// Show turns it into a layer surface, and the web view renders inside it.
//
//	go test -tags 'waylandtest gtk3' ./internal/platform/wayland/wailsproto/
//
// The test re-executes itself under a private headless sway (self-skipping without sway or
// grim). Wails must own the main thread, so the child runs from TestMain.
package wailsproto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/wayland"
)

const childEnv = "RKL_TEST_WAILSPROTO_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) != "" {
		os.Exit(child())
	}
	os.Exit(m.Run())
}

func TestWailsOverlayIsALayerSurface(t *testing.T) {
	for _, bin := range []string{"sway", "swaymsg", "grim"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	dir, err := os.MkdirTemp("", "rkl-wails-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	_ = os.Chmod(dir, 0o700)
	conf := filepath.Join(dir, "sway.conf")
	_ = os.WriteFile(conf, []byte("xwayland disable\noutput * bg #000000 solid_color\n"), 0o600)
	sway := exec.Command("sway", "-c", conf)
	sway.Env = append(baseEnv(), "WLR_BACKENDS=headless", "WLR_LIBINPUT_NO_DEVICES=1",
		"WLR_RENDERER=pixman", "XDG_RUNTIME_DIR="+dir)
	if err := sway.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sway.Process.Kill(); _ = sway.Wait() }()
	var socket, ipc string
	for deadline := time.Now().Add(15 * time.Second); socket == "" || ipc == ""; {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			switch n := e.Name(); {
			case strings.HasPrefix(n, "wayland-") && !strings.HasSuffix(n, ".lock"):
				socket = n
			case strings.HasPrefix(n, "sway-ipc."):
				ipc = filepath.Join(dir, n)
			}
		}
		if time.Now().After(deadline) {
			t.Skip("headless sway did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)

	exe, _ := os.Executable()
	cmd := exec.Command(exe)
	cmd.Env = append(baseEnv(), childEnv+"=1", "XDG_RUNTIME_DIR="+dir, "WAYLAND_DISPLAY="+socket,
		"SWAYSOCK="+ipc, "XDG_SESSION_TYPE=wayland", "XDG_CURRENT_DESKTOP=sway", "GDK_BACKEND=wayland",
		"WEBKIT_DISABLE_COMPOSITING_MODE=1", "LIBGL_ALWAYS_SOFTWARE=1", "WAYLAND_DEBUG="+os.Getenv("RKL_PROTO_WLDEBUG"),
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/rkl-test-bus")
	// RKL_PROTO_JSCSIG=keep leaves JSC on SIGUSR1 (to see the crash MoveJSCSignal prevents).
	if v := os.Getenv("RKL_PROTO_JSCSIG"); v != "" {
		cmd.Env = append(cmd.Env, "RKL_PROTO_JSCSIG="+v)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		err = <-done
	}
	if p := os.Getenv("RKL_PROTO_LOG"); p != "" {
		_ = os.WriteFile(p, out.Bytes(), 0o600)
	}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.HasPrefix(line, "PROTO ") {
			t.Log(line)
		}
	}
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, tail(out.String(), 4000))
	}
}

func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "DISPLAY", "WAYLAND_DISPLAY", "SWAYSOCK", "RKL_BACKEND", "XDG_RUNTIME_DIR", "GDK_BACKEND",
			"XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "DBUS_SESSION_BUS_ADDRESS":
			continue
		}
		env = append(env, kv)
	}
	return env
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// child runs the Wails app on the main goroutine; the scenario runs once it has started.
func child() int {
	// First thing, as the shell should: an unhandled SIGUSR1 would kill us.
	triggers := platform.SignalTriggers(context.Background())
	if os.Getenv("RKL_PROTO_JSCSIG") != "keep" {
		platform.MoveJSCSignal()
	}
	var logs bytes.Buffer
	var logMu sync.Mutex
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return logs.Write(p)
	}), nil)))

	app := application.New(application.Options{
		Name:  "rkl-wailsproto",
		Linux: application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: "rkl-wailsproto"},
	})
	// Created before Run, like the shell's overlay: Wails runs it from a goroutine at startup.
	overlay := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "overlay", Title: "rkl-proto-overlay", Width: 300, Height: 200, Hidden: true, Frameless: true,
		AlwaysOnTop:      true,
		BackgroundColour: application.NewRGBA(255, 0, 0, 255),
		HTML:             `<html><body style="margin:0;background:#ff0000;width:100vw;height:100vh"></body></html>`,
	})

	// A toplevel standing in for the game, under the overlay.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "game", Title: "game", BackgroundColour: application.NewRGBA(0, 0, 255, 255),
		HTML: `<html><body style="background:#0000ff"></body></html>`,
	})

	code := 1
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			defer app.Quit()
			if err := scenario(app, overlay, &logs, &logMu); err != nil {
				fmt.Println("PROTO FAIL:", err)
				return
			}
			if err := signals(triggers); err != nil {
				fmt.Println("PROTO FAIL:", err)
				return
			}
			code = 0
		}()
	})
	if err := app.Run(); err != nil {
		fmt.Println("PROTO app.Run:", err)
		return 1
	}
	return code
}

// signals checks the SIGUSR triggers still arrive once WebKit (JavaScriptCore) is running:
// JSC claims a signal for its GC (SIGUSR1 unless JSC_SIGNAL_FOR_GC says otherwise).
func signals(triggers <-chan platform.HotkeyEvent) error {
	fmt.Println("PROTO JSC_SIGNAL_FOR_GC =", os.Getenv("JSC_SIGNAL_FOR_GC"))
	for _, sig := range []syscall.Signal{syscall.SIGUSR2, syscall.SIGUSR1} {
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			return err
		}
		select {
		case e := <-triggers:
			fmt.Printf("PROTO %v -> %v\n", sig, e.Action.Kind)
		case <-time.After(2 * time.Second):
			fmt.Printf("PROTO %v -> nothing\n", sig)
			return fmt.Errorf("%v trigger lost", sig)
		}
	}
	return nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func scenario(app *application.App, overlay *application.WebviewWindow, logs *bytes.Buffer, logMu *sync.Mutex) error {
	// The pending window is run from a goroutine; wait until its GtkWindow exists.
	deadline := time.Now().Add(10 * time.Second)
	for overlay.NativeWindow() == nil {
		if time.Now().After(deadline) {
			return errors.New("overlay never got a native window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	parts, err := wayland.New(wayland.Options{Probe: platform.EnvProbeFromEnv(), MainThread: application.InvokeSync})
	if err != nil {
		return err
	}
	ov, err := parts.Overlay(overlay.NativeWindow())
	if err != nil {
		return err
	}
	if os.Getenv("RKL_PROTO_NOPREPARE") != "" {
		overlay.Show()
		time.Sleep(3 * time.Second)
		shot, _ := exec.Command("grim", "-t", "png", "-").Output()
		_ = os.WriteFile(os.Getenv("RKL_PROTO_SHOT"), shot, 0o600)
		return errors.New("control run without Prepare")
	}
	if err := ov.Prepare(); err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	logMu.Lock()
	realised := strings.Contains(logs.String(), "already realised")
	logMu.Unlock()
	fmt.Println("PROTO hidden Wails window realised before Prepare:", realised)

	// Show the way the shell would: anchor, then Wails' Show (realize + show_all).
	if err := ov.(platform.LayerAnchorer).AnchorLayer(true, 50, 50); err != nil {
		return err
	}
	if os.Getenv("RKL_PROTO_SHOWAT") != "" {
		if err := ov.ShowAt(platform.Rect{X: 1280 - 50 - 300, Y: 50, Width: 300, Height: 200}); err != nil {
			return err
		}
	} else {
		overlay.Show()
	}
	time.Sleep(3 * time.Second) // web view load and first paint
	alive := make(chan struct{})
	go func() { application.InvokeSync(func() {}); close(alive) }()
	select {
	case <-alive:
		fmt.Println("PROTO main loop alive after Show")
	case <-time.After(3 * time.Second):
		fmt.Println("PROTO main loop BLOCKED after Show")
	}

	tree, err := exec.Command("swaymsg", "-r", "-t", "get_tree").Output()
	if err != nil {
		return err
	}
	if strings.Contains(string(tree), `"name": "rkl-`) {
		return errors.New("overlay is an xdg toplevel, not a layer surface")
	}
	fmt.Println("PROTO overlay absent from sway's toplevel tree (layer surface)")

	shot, err := exec.Command("grim", "-t", "png", "-").Output()
	if err != nil {
		return fmt.Errorf("grim: %w", err)
	}
	if d := os.Getenv("RKL_PROTO_SHOT"); d != "" {
		_ = os.WriteFile(d, shot, 0o600)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		return err
	}
	b := img.Bounds()
	// Centre of a 300x200 surface 50px from the top and right edges.
	x, y := b.Max.X-50-150, 50+100
	r, g, bl, _ := img.At(x, y).RGBA()
	fmt.Printf("PROTO output %dx%d, pixel at (%d,%d) = rgb(%d,%d,%d)\n", b.Dx(), b.Dy(), x, y, r>>8, g>>8, bl>>8)
	if r>>8 < 200 || g>>8 > 60 || bl>>8 > 60 {
		return errors.New("web view content not visible at the anchored spot")
	}
	r, g, bl, _ = img.At(b.Max.X/4, b.Max.Y/2).RGBA()
	if r>>8 > 60 || bl>>8 < 200 {
		return fmt.Errorf("expected the blue toplevel outside the overlay: rgb(%d,%d,%d)", r>>8, g>>8, bl>>8)
	}
	// Hide and anchor left, show again: the surface moves.
	overlay.Hide()
	if err := ov.(platform.LayerAnchorer).AnchorLayer(false, 50, 50); err != nil {
		return err
	}
	overlay.Show()
	time.Sleep(1500 * time.Millisecond)
	shot, err = exec.Command("grim", "-t", "png", "-").Output()
	if err != nil {
		return err
	}
	img, err = png.Decode(bytes.NewReader(shot))
	if err != nil {
		return err
	}
	r, _, _, _ = img.At(50+150, 150).RGBA()
	fmt.Printf("PROTO after re-anchoring left: red=%d at (200,150)\n", r>>8)
	if r>>8 < 200 {
		return errors.New("re-anchored surface not on the left")
	}
	_ = app
	return nil
}
