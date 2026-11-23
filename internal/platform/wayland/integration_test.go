//go:build linux && waylandtest

package wayland

// Wayland-native backend end to end under headless sway:
//
//	go test -tags 'waylandtest gtk3' ./internal/platform/wayland/
//
// The test re-executes itself as a child with a private XDG_RUNTIME_DIR /
// WAYLAND_DISPLAY, no DISPLAY and an unreachable session bus (so the GlobalShortcuts portal is
// deterministically absent and the signal fallback is reported). Self-skipping without sway.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

const childEnv = "DIVVY_TEST_WAYLAND_CHILD"

func TestWaylandBackendEndToEnd(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		child(t)
		return
	}
	if _, err := exec.LookPath("sway"); err != nil {
		t.Skip("sway not installed")
	}
	runtimeDir, err := os.MkdirTemp("", "divvy-wl-")
	require.NoError(t, err)
	defer os.RemoveAll(runtimeDir)
	require.NoError(t, os.Chmod(runtimeDir, 0o700))
	config := filepath.Join(runtimeDir, "sway.conf")
	require.NoError(t, os.WriteFile(config, []byte("xwayland disable\n"), 0o600))

	sway := exec.Command("sway", "-c", config)
	sway.Env = append(cleanEnv(),
		"WLR_BACKENDS=headless", "WLR_LIBINPUT_NO_DEVICES=1", "WLR_RENDERER=pixman",
		"XDG_RUNTIME_DIR="+runtimeDir)
	require.NoError(t, sway.Start())
	defer func() { _ = sway.Process.Kill(); _ = sway.Wait() }()

	var socket, ipc string
	deadline := time.Now().Add(15 * time.Second)
	for socket == "" || ipc == "" {
		entries, _ := os.ReadDir(runtimeDir)
		for _, e := range entries {
			n := e.Name()
			switch {
			case strings.HasPrefix(n, "wayland-") && !strings.HasSuffix(n, ".lock"):
				socket = n
			case strings.HasPrefix(n, "sway-ipc.") && strings.HasSuffix(n, ".sock"):
				ipc = filepath.Join(runtimeDir, n)
			}
		}
		if time.Now().After(deadline) {
			t.Skip("headless sway did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The socket appears before sway finishes initialising its globals.
	time.Sleep(500 * time.Millisecond)

	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run", "^TestWaylandBackendEndToEnd$", "-test.v", "-test.count=1")
	cmd.Env = append(cleanEnv(),
		childEnv+"=1",
		"XDG_RUNTIME_DIR="+runtimeDir,
		"WAYLAND_DISPLAY="+socket,
		"SWAYSOCK="+ipc,
		"XDG_SESSION_TYPE=wayland",
		"XDG_CURRENT_DESKTOP=sway",
		"GDK_BACKEND=wayland",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/divvy-test-bus",
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	require.NoError(t, cmd.Run(), "wayland child test failed")
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "DISPLAY", "WAYLAND_DISPLAY", "SWAYSOCK", "DIVVY_BACKEND", "XDG_RUNTIME_DIR", "GDK_BACKEND",
			"XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "DBUS_SESSION_BUS_ADDRESS":
			continue
		}
		env = append(env, kv)
	}
	return env
}

func swaymsg(t *testing.T, kind string) string {
	t.Helper()
	out, err := exec.Command("swaymsg", "-r", "-t", kind).Output()
	require.NoError(t, err)
	return string(out)
}

func child(t *testing.T) {
	t.Helper()
	// GTK must stay on one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// No DISPLAY: Wayland-native.
	assert.Equal(t, platform.BackendWayland, platform.Detect(platform.EnvProbeFromEnv(), ""))

	// Signals first, so a stray SIGUSR cannot kill the child.
	events := platform.SignalTriggers(t.Context())

	parts, err := New(Options{Probe: platform.EnvProbeFromEnv()})
	require.NoError(t, err)
	assert.Equal(t, platform.BackendWayland, parts.Kind)
	caps := parts.Capabilities
	assert.False(t, caps.WindowTracking || caps.PassiveHook, "%+v", caps)
	assert.True(t, caps.OverlayAlwaysOnTop && caps.OverlayClickThrough, "%+v", caps)
	assert.True(t, containsText(caps.Notes, "anchored to the output"), "%+v", caps)

	// Keystrokes through our own virtual keyboard: it uploads its own keymap, so it works even
	// on headless sway's keyboard-less seat.
	assert.True(t, caps.KeystrokeInjection, "%+v", caps)
	vk, isVK := parts.InputSimulator.(*virtualKeyboard)
	require.True(t, isVK)
	if vk.ready() {
		require.NoError(t, parts.InputSimulator.SendChord(platform.NewChord("KeyC", platform.Ctrl)))
		require.NoError(t, parts.InputSimulator.SendChord(platform.NewChord("F5", platform.Ctrl, platform.Shift)))
		assert.Contains(t, swaymsg(t, "get_inputs"), `"type": "keyboard"`, "virtual keyboard device")
	} else {
		t.Logf("NOTE: virtual keyboard not ready: %v", caps.Notes)
	}
	err = parts.InputSimulator.SendChord(platform.NewChord("NoSuchKey"))
	require.ErrorIs(t, err, platform.ErrInvalidChord)

	// No portal on a dead bus: the signal fallback is reported.
	ok, notes := GlobalHotkeys(t.Context())
	assert.False(t, ok)
	assert.True(t, containsText(notes, "--trigger"), notes)

	// Unsupported pieces report themselves.
	_, found := parts.WindowTracker.GameWindow()
	assert.False(t, found)
	assert.True(t, parts.WindowTracker.IsGameFocused())
	_, err = parts.WindowTracker.Subscribe(t.Context())
	require.ErrorIs(t, err, platform.ErrUnsupported)
	_, err = parts.InputListener.Start(t.Context())
	require.ErrorIs(t, err, platform.ErrUnsupported)

	// Signal triggers, as `pkill -USR2 divvy` / `-USR1` would send them.
	require.NoError(t, exec.Command("kill", "-USR2", strconv.Itoa(os.Getpid())).Run())
	assert.Equal(t, platform.Action(platform.PriceCheck), recvTrigger(t, events).Action)
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	assert.Equal(t, platform.Action(platform.PriceCheckPinned), recvTrigger(t, events).Action)

	layerShellScenario(t, parts)
}

// containsText reports whether any entry of list contains sub.
func containsText(list []string, sub string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.Contains(s, sub) })
}

func recvTrigger(t *testing.T, ch <-chan platform.HotkeyEvent) platform.HotkeyEvent {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "stream closed")
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no trigger within 5s")
	}
	return platform.HotkeyEvent{}
}
