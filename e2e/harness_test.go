//go:build e2e

package e2e

import (
	"bufio"
	"context"
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
var mock *mockTrade

// artifacts keeps the app and fake-game logs.
var artifacts string

func TestMain(m *testing.M) {
	mock = newMockTrade()
	dir, keep := os.LookupEnv("DIVVY_E2E_ARTIFACTS")
	if !keep {
		var err error
		if dir, err = os.MkdirTemp("", "divvy-e2e-"); err != nil {
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
	return binary(t, "DIVVY_E2E_APP", filepath.Join("target", "go", exe("divvy-e2e")),
		"build it with `pnpm --dir web build && go build -tags gtk3,testhook -o target/go/divvy-e2e ./cmd/divvy` (drop gtk3 off Linux)")
}

func fakeGameBinary(t *testing.T) string {
	return binary(t, "DIVVY_E2E_FAKE_GAME", filepath.Join("target", "e2e", exe("fake-game")),
		"build it with `go build -o target/e2e/fake-game ./e2e/cmd/fake-game`")
}

func fixture(name string) string { return filepath.Join(repoRoot(), "e2e", "fixtures", name) }

func timeout() time.Duration {
	if s, err := strconv.Atoi(os.Getenv("DIVVY_E2E_TIMEOUT_SECS")); err == nil && s > 0 {
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

// hookClient talks to the app's test hook: one connection per command, one reply line each.
type hookClient struct {
	port  int
	token string
}

// Command sends cmd and returns the reply line.
func (h hookClient) Command(cmd string) (string, error) {
	// `dom` waits up to 5 s for the web view; leave room for that.
	return h.command(cmd, 10*time.Second)
}

func (h hookClient) command(cmd string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(h.port)))
	if err != nil {
		return "", fmt.Errorf("test hook on 127.0.0.1:%d: %w", h.port, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
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
func (h hookClient) OK(t *testing.T, cmd string) {
	t.Helper()
	r, err := h.Command(cmd)
	require.NoError(t, err)
	require.Equal(t, "ok", r, "test hook %q", firstWords(cmd))
}

// appState is the hook's `state` reply.
type appState struct {
	OverlayVisible  bool          `json:"overlay_visible"`
	OverlayPinned   bool          `json:"overlay_pinned"`
	SettingsVisible bool          `json:"settings_visible"`
	NoticeVisible   bool          `json:"notice_visible"`
	TrayPresent     bool          `json:"tray_present"`
	CanCopy         bool          `json:"can_copy"`
	LastItem        *string       `json:"last_item"`
	LastPrice       *priceSummary `json:"last_price"`
}

// priceSummary is the overlay's price summary.
type priceSummary struct {
	Total  int    `json:"total"`
	Median *price `json:"median"`
	Min    *price `json:"min"`
	Max    *price `json:"max"`
}

// price is an amount of a currency.
type price struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

func (s appState) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// State fetches `state`.
func (h hookClient) State() (appState, error) {
	r, err := h.Command("state")
	if err != nil {
		return appState{}, err
	}
	var s appState
	if err := json.Unmarshal([]byte(r), &s); err != nil {
		return appState{}, fmt.Errorf("`state` is not JSON: %s", r)
	}
	return s, nil
}

// WaitState polls `state` until ok holds.
func (h hookClient) WaitState(t *testing.T, what string, timeout time.Duration, ok func(appState) bool) appState {
	t.Helper()
	var last appState
	var err error
	if !poll(timeout, 200*time.Millisecond, func() bool {
		last, err = h.State()
		return err == nil && ok(last)
	}) {
		t.Fatalf("timed out after %s waiting for %s; last state: %s (err %v)", timeout, what, last, err)
	}
	return last
}

// domReply is the reply to `dom`.
type domReply struct {
	Exists  bool   `json:"exists"`
	Visible bool   `json:"visible"`
	Text    string `json:"text"`
}

// DOM asks the web view of window ("overlay" or "settings") about selector.
func (h hookClient) DOM(window, selector string) (domReply, error) {
	r, err := h.Command("dom " + window + " " + selector)
	if err != nil {
		return domReply{}, err
	}
	var d domReply
	if err := json.Unmarshal([]byte(r), &d); err != nil {
		return domReply{}, fmt.Errorf("`dom` replied %s", r)
	}
	return d, nil
}

// WaitDOM polls `dom` until ok holds.
func (h hookClient) WaitDOM(t *testing.T, window, selector string, timeout time.Duration, ok func(domReply) bool) domReply {
	t.Helper()
	var last domReply
	var err error
	if !poll(timeout, 200*time.Millisecond, func() bool {
		last, err = h.DOM(window, selector)
		return err == nil && ok(last)
	}) {
		t.Fatalf("timed out after %s waiting for %s %q; last: %+v (err %v)", timeout, window, selector, last, err)
	}
	return last
}

// poll calls try every interval until it returns true (true) or timeout has passed (false).
func poll(timeout, interval time.Duration, try func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if try() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

// waitDelay bounds how long Wait waits for a killed child's output pipes to close.
const waitDelay = 5 * time.Second

// Settings fetches the settings as generic JSON.
func (h hookClient) Settings(t *testing.T) map[string]any {
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

// appProcess is one running app process.
type appProcess struct {
	hookClient
	cmd    *exec.Cmd
	exited chan struct{}
	log    string
	// lastTrigger is when the previous triggerPriceCheck fired.
	lastTrigger time.Time
}

// appSession is an isolated settings directory the app can be started in, more than once.
type appSession struct {
	t         *testing.T
	ConfigDir string
	starts    int
}

// newSession creates a fresh DIVVY_CONFIG_DIR with onboarding marked done (onboarding has its
// own web tests; here it would open the settings window over everything).
func newSession(t *testing.T) *appSession {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "settings.toml"),
		[]byte("version = 1\n\n[ui]\nonboarding_done = true\n"), 0o644))
	return &appSession{t: t, ConfigDir: dir}
}

// SettingsFile is the settings file in this session.
func (s *appSession) SettingsFile() string {
	return filepath.Join(s.ConfigDir, "config", "settings.toml")
}

// Start launches the app and waits for its test hook. t is the calling (sub)test; the app
// lives until the session's test ends.
func (s *appSession) Start(t *testing.T) *appProcess {
	t.Helper()
	bin := appBinary(t)
	s.starts++
	port := hookPort()
	portFile := filepath.Join(t.TempDir(), "hook-port")
	token := randomToken(t)
	logPath := filepath.Join(artifacts, fmt.Sprintf("%s-app-%d.log", safeName(t.Name()), s.starts))
	logFile, err := os.Create(logPath)
	require.NoError(t, err)

	// The app lives until the session's test ends, not just the calling subtest.
	cmd := exec.CommandContext(s.t.Context(), bin)
	cmd.WaitDelay = waitDelay
	// Later entries win over the inherited environment.
	cmd.Env = append(os.Environ(),
		"DIVVY_TEST_HOOK="+strconv.Itoa(port),
		"DIVVY_TEST_HOOK_PORT_FILE="+portFile,
		"DIVVY_TEST_HOOK_TOKEN="+token,
		"DIVVY_CONFIG_DIR="+s.ConfigDir,
		"DIVVY_TRADE_BASE_URL="+mock.URL(),
		"DIVVY_SCOUT_BASE_URL="+mock.URL(),
		"DIVVY_NINJA_BASE_URL="+mock.URL(),
		"DIVVY_LOG="+envOr("DIVVY_LOG", "debug"),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start(), "starting %s", bin)
	a := &appProcess{port: port, token: token, cmd: cmd, exited: make(chan struct{}), log: logPath}
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

	// The app writes the port it bound (port 0: one the OS picked) once the hook listens.
	var hookErr error
	up := poll(timeout(), 250*time.Millisecond, func() bool {
		if a.port == 0 {
			a.port, hookErr = readPortFile(portFile)
			if hookErr != nil {
				return a.Exited()
			}
		}
		_, hookErr = a.command("state", 2*time.Second)
		return hookErr == nil || a.Exited()
	})
	switch {
	case hookErr == nil:
	case a.Exited():
		t.Fatalf("the app exited before its test hook came up; see %s", logPath)
	case !up:
		t.Fatalf("test hook not reachable after %s: %v; is %s a test-hook build (-tags testhook)?", timeout(), hookErr, bin)
	}
	return a
}

// Exited reports whether the process has exited.
func (a *appProcess) Exited() bool {
	select {
	case <-a.exited:
		return true
	default:
		return false
	}
}

// WaitExit waits for the process to exit.
func (a *appProcess) WaitExit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-a.exited:
	case <-time.After(timeout):
		t.Fatalf("the app is still running %s later", timeout)
	}
}

// Quit sends `quit` and waits for the process to exit.
func (a *appProcess) Quit(t *testing.T) {
	t.Helper()
	a.OK(t, "quit")
	a.WaitExit(t, 10*time.Second)
}

// Kill stops the process if it is still running.
func (a *appProcess) Kill() {
	if !a.Exited() {
		_ = a.cmd.Process.Kill()
		<-a.exited
	}
}

// Pid is the app's process id.
func (a *appProcess) Pid() int { return a.cmd.Process.Pid }

// WaitOverlayLoaded waits until the (hidden) overlay web view has rendered the app, so the
// first hotkey press is not lost on a cold web view.
func (a *appProcess) WaitOverlayLoaded(t *testing.T) {
	t.Helper()
	a.WaitDOM(t, "overlay", `main[data-route="overlay"]`, timeout(), func(d domReply) bool { return d.Exists })
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// hookPort is the fixed hook port from DIVVY_TEST_HOOK, else 0: the app binds a free port and
// reports it in DIVVY_TEST_HOOK_PORT_FILE.
func hookPort() int {
	if p, err := strconv.Atoi(os.Getenv("DIVVY_TEST_HOOK")); err == nil && p > 0 {
		return p
	}
	return 0
}

// readPortFile reads the port the app wrote to path.
func readPortFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("waiting for the hook port: %w", err)
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func randomToken(t *testing.T) string {
	if tok := os.Getenv("DIVVY_TEST_HOOK_TOKEN"); tok != "" {
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

// gameProcess is a running fake game window (e2e/cmd/fake-game).
type gameProcess struct {
	cmd    *exec.Cmd
	lines  chan string
	exited chan struct{}

	mu     sync.Mutex
	copies int
	log    string
}

// startGame starts the fake game serving the item text file and waits for READY.
func startGame(t *testing.T, item string) *gameProcess {
	t.Helper()
	bin := fakeGameBinary(t)
	logPath := filepath.Join(artifacts, safeName(t.Name())+"-game.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), bin, "--exit-after", "300", "--item", item)
	cmd.WaitDelay = waitDelay
	cmd.Stderr = logFile
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start(), "starting %s", bin)
	g := &gameProcess{cmd: cmd, lines: make(chan string, 64), exited: make(chan struct{}), log: logPath}
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
func (g *gameProcess) Copies() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.copies
}

// WaitCopied waits until the game has served at least n copies; describe adds context to
// the failure.
func (g *gameProcess) WaitCopied(t *testing.T, n int, timeout time.Duration, describe func() string) {
	t.Helper()
	if !poll(timeout, 100*time.Millisecond, func() bool { return g.Copies() >= n }) {
		t.Fatalf("fake-game copied %d times, want %d: the app never sent the copy chord to the game "+
			"(trigger %s; focus? permissions?); %s", g.Copies(), n, triggerMode(), describe())
	}
}

// Stop ends the game and waits for it.
func (g *gameProcess) Stop() {
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

// triggerMode is DIVVY_E2E_TRIGGER resolved: hook, xdotool or signal.
func triggerMode() string {
	switch m := os.Getenv("DIVVY_E2E_TRIGGER"); m {
	case "hook", "xdotool", "signal":
		return m
	}
	if isX11() {
		return "xdotool"
	}
	return "hook"
}

// focusGame brings the fake game to the front: DIVVY_E2E_FOCUS_CMD, else xdotool on X11.
func focusGame(t *testing.T) {
	t.Helper()
	cmd := os.Getenv("DIVVY_E2E_FOCUS_CMD")
	if cmd == "" && isX11() {
		cmd = `xdotool search --sync --name '^Path of Exile 2$' windowactivate --sync`
	}
	if cmd == "" {
		return
	}
	// The compositor can take a moment to manage a window that has just been mapped (sway with
	// XWayland), so retry while the window is not there yet.
	var out []byte
	var err error
	if !poll(5*time.Second, 200*time.Millisecond, func() bool {
		out, err = shell(t, cmd).CombinedOutput()
		return err == nil
	}) {
		t.Fatalf("focusing the game: `%s`: %v: %s", cmd, err, out)
	}
}

// shell is a `sh -c` command, ended with the test.
func shell(t *testing.T, cmd string) *exec.Cmd {
	c := exec.CommandContext(t.Context(), "sh", "-c", cmd)
	c.WaitDelay = waitDelay
	return c
}

// run runs a `sh -c` command.
func run(t *testing.T, cmd string) {
	t.Helper()
	out, err := shell(t, cmd).CombinedOutput()
	require.NoError(t, err, "`%s`: %s", cmd, out)
}

// triggerSpacing is the least time triggerPriceCheck leaves between two triggers of one app.
// The app drops presses that come less than its debounce (150 ms) after the previous price
// check started, as key bounce; on a fast run (Wayland, no focus delay) a second trigger
// could land inside it.
const triggerSpacing = 300 * time.Millisecond

// triggerPriceCheck starts a price check the way DIVVY_E2E_TRIGGER says, with the game focused
// (the app sends its copy chord to whatever has focus, as for a real player).
func triggerPriceCheck(t *testing.T, a *appProcess, pinned bool) {
	t.Helper()
	focusGame(t)
	// The app copies only once it sees the game focused (its tracker polls, and a notice or a
	// just-hidden panel may hold focus for a moment); a trigger before that is dropped.
	a.WaitState(t, "the game focused", timeout(), func(s appState) bool { return s.CanCopy })
	if wait := triggerSpacing - time.Since(a.lastTrigger); wait > 0 {
		time.Sleep(wait)
	}
	defer func() { a.lastTrigger = time.Now() }()
	switch triggerMode() {
	case "xdotool":
		// The app grabs its hotkeys only while it sees the game focused (it polls for that);
		// a press right after focusing would beat the grab.
		time.Sleep(time.Second)
		chord := envOr("DIVVY_E2E_HOTKEY", "ctrl+d")
		if pinned {
			chord = envOr("DIVVY_E2E_HOTKEY_PINNED", "ctrl+alt+d")
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
