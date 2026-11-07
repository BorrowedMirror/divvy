//go:build e2e

package e2e

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mock is the trade2 mock shared by every test; tests that depend on its listings reset and
// configure it first.
var mock *MockTrade

// artifacts keeps the app and fake-game logs.
var artifacts string

func TestMain(m *testing.M) {
	mock = NewMockTrade()
	dir, keep := os.LookupEnv("RKL_E2E_ARTIFACTS")
	if !keep {
		var err error
		if dir, err = os.MkdirTemp("", "rkl-e2e-"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	artifacts, _ = filepath.Abs(dir)
	code := m.Run()
	mock.Close()
	if !keep {
		_ = os.RemoveAll(artifacts)
	}
	os.Exit(code)
}

// repoRoot is the repository root (`go test` runs in e2e/).
func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Dir(wd)
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// binary is the path in env, else def; relative paths are relative to the repository root.
func binary(t *testing.T, env, def, hint string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		p = def
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(repoRoot(), p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s not found (%s); %s", p, env, hint)
	}
	return p
}

func appBinary(t *testing.T) string {
	return binary(t, "RKL_E2E_APP", filepath.Join("target", "go", exe("rkl-e2e")),
		"build it with `pnpm --dir web build && go build -tags gtk3,testhook -o target/go/rkl-e2e ./cmd/rkl` (drop gtk3 off Linux)")
}

func fakeGameBinary(t *testing.T) string {
	return binary(t, "RKL_E2E_FAKE_GAME", filepath.Join("target", "e2e", "debug", exe("fake-game")),
		"build it with `cargo build -p fake-game --target-dir target/e2e`")
}

func fixture(name string) string { return filepath.Join(repoRoot(), "e2e", "fixtures", name) }

func timeout() time.Duration {
	if s, err := strconv.Atoi(os.Getenv("RKL_E2E_TIMEOUT_SECS")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 30 * time.Second
}

// isX11 is a Linux X11 session (XWayland counts when WAYLAND_DISPLAY is hidden from the tests).
func isX11() bool {
	return runtime.GOOS == "linux" && os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

// ---------------------------------------------------------------------------------------------
// Test hook client
// ---------------------------------------------------------------------------------------------

// Hook talks to the app's test hook: one connection per command, one reply line each.
type Hook struct {
	port  int
	token string
}

// Command sends cmd and returns the reply line.
func (h Hook) Command(cmd string) (string, error) {
	// `dom` waits up to 5 s for the web view; leave room for that.
	return h.command(cmd, 10*time.Second)
}

func (h Hook) command(cmd string, timeout time.Duration) (string, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(h.port)), 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("test hook on 127.0.0.1:%d: %w", h.port, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(conn, "auth %s\n%s\n", h.token, cmd); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading the reply to %q: %w", firstWords(cmd), err)
	}
	return strings.TrimSpace(line), nil
}

func firstWords(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

// OK sends cmd and fails unless the reply is `ok`.
func (h Hook) OK(t *testing.T, cmd string) {
	t.Helper()
	r, err := h.Command(cmd)
	require.NoError(t, err)
	require.Equal(t, "ok", r, "test hook %q", firstWords(cmd))
}

// State is the hook's `state` reply.
type State struct {
	OverlayVisible  bool          `json:"overlay_visible"`
	OverlayPinned   bool          `json:"overlay_pinned"`
	SettingsVisible bool          `json:"settings_visible"`
	TrayPresent     bool          `json:"tray_present"`
	LastItem        *string       `json:"last_item"`
	LastPrice       *PriceSummary `json:"last_price"`
}

// PriceSummary is the overlay's price summary.
type PriceSummary struct {
	Total  int    `json:"total"`
	Median *Price `json:"median"`
	Min    *Price `json:"min"`
	Max    *Price `json:"max"`
}

// Price is an amount of a currency.
type Price struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

func (s State) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// State fetches `state`.
func (h Hook) State() (State, error) {
	r, err := h.Command("state")
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal([]byte(r), &s); err != nil {
		return State{}, fmt.Errorf("`state` is not JSON: %s", r)
	}
	return s, nil
}

// WaitState polls `state` until ok holds.
func (h Hook) WaitState(t *testing.T, what string, timeout time.Duration, ok func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last State
	var err error
	for time.Now().Before(deadline) {
		if last, err = h.State(); err == nil && ok(last) {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s; last state: %s (err %v)", timeout, what, last, err)
	return last
}

// DOM is the reply to `dom`.
type DOM struct {
	Exists  bool   `json:"exists"`
	Visible bool   `json:"visible"`
	Text    string `json:"text"`
}

// DOM asks the web view of window ("overlay" or "settings") about selector.
func (h Hook) DOM(window, selector string) (DOM, error) {
	r, err := h.Command("dom " + window + " " + selector)
	if err != nil {
		return DOM{}, err
	}
	var d DOM
	if err := json.Unmarshal([]byte(r), &d); err != nil {
		return DOM{}, fmt.Errorf("`dom` replied %s", r)
	}
	return d, nil
}

// WaitDOM polls `dom` until ok holds.
func (h Hook) WaitDOM(t *testing.T, window, selector string, timeout time.Duration, ok func(DOM) bool) DOM {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last DOM
	var err error
	for time.Now().Before(deadline) {
		if last, err = h.DOM(window, selector); err == nil && ok(last) {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s %q; last: %+v (err %v)", timeout, window, selector, last, err)
	return last
}

// Settings fetches the settings as generic JSON.
func (h Hook) Settings(t *testing.T) map[string]any {
	t.Helper()
	r, err := h.Command("settings")
	require.NoError(t, err)
	var s map[string]any
	require.NoError(t, json.Unmarshal([]byte(r), &s), "`settings` replied %s", r)
	return s
}

// ---------------------------------------------------------------------------------------------
// The app
// ---------------------------------------------------------------------------------------------

// App is one running app process.
type App struct {
	Hook
	cmd    *exec.Cmd
	exited chan struct{}
	log    string
}

// Session is an isolated settings directory the app can be started in, more than once.
type Session struct {
	t         *testing.T
	ConfigDir string
	starts    int
}

// NewSession creates a fresh RKL_CONFIG_DIR with onboarding marked done (onboarding has its
// own web tests; here it would open the settings window over everything).
func NewSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "settings.toml"),
		[]byte("version = 1\n\n[ui]\nonboarding_done = true\n"), 0o644))
	return &Session{t: t, ConfigDir: dir}
}

// SettingsFile is the settings file in this session.
func (s *Session) SettingsFile() string { return filepath.Join(s.ConfigDir, "config", "settings.toml") }

// Start launches the app and waits for its test hook. t is the calling (sub)test; the app
// lives until the session's test ends.
func (s *Session) Start(t *testing.T) *App {
	t.Helper()
	bin := appBinary(t)
	s.starts++
	port := hookPort(t)
	token := randomToken(t)
	logPath := filepath.Join(artifacts, fmt.Sprintf("%s-app-%d.log", safeName(t.Name()), s.starts))
	logFile, err := os.Create(logPath)
	require.NoError(t, err)

	cmd := exec.Command(bin)
	cmd.Env = appEnv(map[string]string{
		"RKL_TEST_HOOK":       strconv.Itoa(port),
		"RKL_TEST_HOOK_TOKEN": token,
		"RKL_CONFIG_DIR":      s.ConfigDir,
		"RKL_TRADE_BASE_URL":  mock.URL(),
		"RKL_SCOUT_BASE_URL":  mock.URL(),
		"RKL_NINJA_BASE_URL":  mock.URL(),
		"RKL_LOG":             envOr("RKL_LOG", "debug"),
	})
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start(), "starting %s", bin)
	a := &App{Hook: Hook{port: port, token: token}, cmd: cmd, exited: make(chan struct{}), log: logPath}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(a.exited)
	}()
	s.t.Cleanup(func() {
		a.Kill()
		if s.t.Failed() {
			dumpLog(s.t, "app", logPath)
		}
	})

	deadline := time.Now().Add(timeout())
	for {
		if _, err := a.command("state", 2*time.Second); err == nil {
			break
		} else if a.Exited() {
			t.Fatalf("the app exited before its test hook came up; see %s", logPath)
		} else if time.Now().After(deadline) {
			t.Fatalf("test hook not reachable after %s: %v; is %s a test-hook build (-tags testhook)?", timeout(), err, bin)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return a
}

// Exited reports whether the process has exited.
func (a *App) Exited() bool {
	select {
	case <-a.exited:
		return true
	default:
		return false
	}
}

// WaitExit waits for the process to exit.
func (a *App) WaitExit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-a.exited:
	case <-time.After(timeout):
		t.Fatalf("the app is still running %s later", timeout)
	}
}

// Quit sends `quit` and waits for the process to exit.
func (a *App) Quit(t *testing.T) {
	t.Helper()
	a.OK(t, "quit")
	a.WaitExit(t, 10*time.Second)
}

// Kill stops the process if it is still running.
func (a *App) Kill() {
	if !a.Exited() {
		_ = a.cmd.Process.Kill()
		<-a.exited
	}
}

// Pid is the app's process id.
func (a *App) Pid() int { return a.cmd.Process.Pid }

// WaitOverlayLoaded waits until the (hidden) overlay web view has rendered the app, so the
// first hotkey press is not lost on a cold web view.
func (a *App) WaitOverlayLoaded(t *testing.T) {
	t.Helper()
	a.WaitDOM(t, "overlay", `main[data-route="overlay"]`, timeout(), func(d DOM) bool { return d.Exists })
}

func appEnv(set map[string]string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok {
			env = append(env, kv)
		}
	}
	for k, v := range set {
		env = append(env, k+"="+v)
	}
	return env
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func hookPort(t *testing.T) int {
	if p, err := strconv.Atoi(os.Getenv("RKL_TEST_HOOK")); err == nil && p > 0 {
		return p
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func randomToken(t *testing.T) string {
	if tok := os.Getenv("RKL_TEST_HOOK_TOKEN"); tok != "" {
		return tok
	}
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ' ' || r == ':' {
			return '_'
		}
		return r
	}, s)
}

func dumpLog(t *testing.T, what, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	t.Logf("--- %s log %s (last %d lines)\n%s", what, path, len(lines), strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------------------------
// Fake game
// ---------------------------------------------------------------------------------------------

// Game is a running tools/fake-game window.
type Game struct {
	cmd    *exec.Cmd
	lines  chan string
	exited chan struct{}

	mu     sync.Mutex
	copies int
	log    string
}

// StartGame starts the fake game serving the item text file and waits for READY.
func StartGame(t *testing.T, item string) *Game {
	t.Helper()
	bin := fakeGameBinary(t)
	logPath := filepath.Join(artifacts, safeName(t.Name())+"-game.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	cmd := exec.Command(bin, "--exit-after", "300", "--item", item)
	cmd.Stderr = logFile
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start(), "starting %s", bin)
	g := &Game{cmd: cmd, lines: make(chan string, 64), exited: make(chan struct{}), log: logPath}
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line := sc.Text()
			fmt.Fprintln(logFile, line)
			if n, ok := strings.CutPrefix(line, "COPIED "); ok {
				c, _ := strconv.Atoi(n)
				g.mu.Lock()
				g.copies = c
				g.mu.Unlock()
			}
			select {
			case g.lines <- line:
			default:
			}
		}
		_ = cmd.Wait()
		_ = logFile.Close()
		close(g.exited)
	}()
	t.Cleanup(func() {
		g.Stop()
		if t.Failed() {
			dumpLog(t, "fake-game", logPath)
		}
	})
	deadline := time.After(15 * time.Second)
	for {
		select {
		case l := <-g.lines:
			if l == "READY" {
				return g
			}
		case <-g.exited:
			t.Fatalf("fake-game exited before READY; see %s", logPath)
		case <-deadline:
			t.Fatalf("fake-game did not print READY within 15s; see %s", logPath)
		}
	}
}

// Copies is how many times the game served the item (the app sent it the copy chord).
func (g *Game) Copies() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.copies
}

// WaitCopied waits until the game has served at least n copies.
func (g *Game) WaitCopied(t *testing.T, n int, timeout time.Duration, context func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for g.Copies() < n {
		if time.Now().After(deadline) {
			t.Fatalf("fake-game copied %d times, want %d: the app never sent the copy chord to the game "+
				"(trigger %s; focus? permissions?); %s", g.Copies(), n, triggerMode(), context())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Stop ends the game and waits for it.
func (g *Game) Stop() {
	select {
	case <-g.exited:
	default:
		_ = g.cmd.Process.Kill()
		<-g.exited
	}
}

// ---------------------------------------------------------------------------------------------
// Triggering a price check
// ---------------------------------------------------------------------------------------------

// triggerMode is RKL_E2E_TRIGGER resolved: hook, xdotool or signal.
func triggerMode() string {
	switch m := os.Getenv("RKL_E2E_TRIGGER"); m {
	case "hook", "xdotool", "signal":
		return m
	}
	if isX11() {
		return "xdotool"
	}
	return "hook"
}

// FocusGame brings the fake game to the front: RKL_E2E_FOCUS_CMD, else xdotool on X11.
func FocusGame(t *testing.T) {
	t.Helper()
	cmd := os.Getenv("RKL_E2E_FOCUS_CMD")
	if cmd == "" && isX11() {
		cmd = `xdotool search --sync --name '^Path of Exile 2$' windowactivate --sync`
	}
	if cmd == "" {
		return
	}
	// The compositor can take a moment to manage a window that has just been mapped (sway with
	// XWayland), so retry while the window is not there yet.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("focusing the game: `%s`: %v: %s", cmd, err, out)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// run runs a `sh -c` command.
func run(t *testing.T, cmd string) {
	t.Helper()
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	require.NoError(t, err, "`%s`: %s", cmd, out)
}

// lastTrigger is when the previous TriggerPriceCheck fired. The app drops presses that come
// less than its debounce (150 ms) after the previous price check started, as key bounce; on a
// fast run (Wayland, no focus delay) a second trigger could land inside it.
var lastTrigger time.Time

// triggerSpacing is the least time TriggerPriceCheck leaves between two triggers.
const triggerSpacing = 300 * time.Millisecond

// TriggerPriceCheck starts a price check the way RKL_E2E_TRIGGER says, with the game focused
// (the app sends its copy chord to whatever has focus, as for a real player).
func TriggerPriceCheck(t *testing.T, a *App, pinned bool) {
	t.Helper()
	FocusGame(t)
	if wait := triggerSpacing - time.Since(lastTrigger); wait > 0 {
		time.Sleep(wait)
	}
	defer func() { lastTrigger = time.Now() }()
	switch triggerMode() {
	case "xdotool":
		// The app grabs its hotkeys only while it sees the game focused (it polls for that);
		// a press right after focusing would beat the grab.
		time.Sleep(time.Second)
		chord := envOr("RKL_E2E_HOTKEY", "ctrl+d")
		if pinned {
			chord = envOr("RKL_E2E_HOTKEY_PINNED", "ctrl+alt+d")
		}
		run(t, "xdotool key --clearmodifiers "+chord)
	case "signal":
		require.NoError(t, signalTrigger(a.Pid(), pinned))
	default:
		arg := "0"
		if pinned {
			arg = "1"
		}
		a.OK(t, "trigger "+arg)
	}
}

// steps runs ordered subtests that depend on each other: once one fails, the rest are skipped.
func steps(t *testing.T, list ...step) {
	t.Helper()
	for _, s := range list {
		t.Run(s.name, func(st *testing.T) {
			if t.Failed() {
				st.Skip("an earlier step failed")
			}
			s.fn(st)
		})
	}
}

type step struct {
	name string
	fn   func(t *testing.T)
}
