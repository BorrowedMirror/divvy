// Command fake-game opens a window titled "Path of Exile 2". On Ctrl+C / Ctrl+Alt+C it puts
// the chosen corpus item (normal / advanced variant) on the clipboard, like the real game does.
// The e2e tests (e2e/, e2e/scripts) and the X11 backend's integration tests drive it.
//
//	go build -o target/e2e/fake-game ./e2e/cmd/fake-game
//	fake-game (--item <file> | --corpus-id <id>) [--advanced-item <file> | --advanced-corpus-id <id>]
//	          [--corpus-dir <dir>] [--exit-after <secs>] [--size WxH] [--log-keys]
//
// Stdout protocol (one line per write): `READY` once the window has been drawn and mapped,
// `COPIED <n>` after the n-th copy (n starts at 1), `EXIT` before exiting. With --log-keys it
// also prints `KEY <mods+code>` for every key press and `PASTE <text>` on Ctrl+V.
// FAKE_GAME_DEBUG=1 logs window events to stderr.
//
// Backends: X11 (also XWayland) over xgb, native Wayland over libwayland-client (cgo, used when
// WAYLAND_DISPLAY is set), Win32 and AppKit (purego). The clipboard stays owned for the whole
// run: on X11 and Wayland the selection owner serves the data itself.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core/coretest"
)

const title = "Path of Exile 2"

// options are the parsed command line.
type options struct {
	item, advancedItem         string
	corpusID, advancedCorpusID string
	corpusDir                  string
	exitAfter                  float64
	hasExitAfter               bool
	width, height              uint32
	logKeys                    bool
}

// parseArgs parses args (without the program name). getenv supplies RKL_CORPUS_DIR.
func parseArgs(args []string, getenv func(string) string) (options, error) {
	o := options{width: 1280, height: 720}
	fs := flag.NewFlagSet("fake-game", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.item, "item", "", "item text file served on Ctrl+C")
	fs.StringVar(&o.advancedItem, "advanced-item", "", "item text file served on Ctrl+Alt+C (defaults to the normal item)")
	fs.StringVar(&o.corpusID, "corpus-id", "", "corpus id (e.g. currency/divine-orb) served on Ctrl+C")
	fs.StringVar(&o.advancedCorpusID, "advanced-corpus-id", "", "corpus id served on Ctrl+Alt+C")
	fs.StringVar(&o.corpusDir, "corpus-dir", "", "corpus directory (env RKL_CORPUS_DIR; default: the in-repo corpus)")
	fs.Func("exit-after", "exit after this many seconds (CI safety net)", func(s string) error {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		o.exitAfter, o.hasExitAfter = v, true
		return nil
	})
	fs.Func("size", "window size WxH (default 1280x720)", func(s string) error {
		w, h, err := parseSize(s)
		o.width, o.height = w, h
		return err
	})
	fs.BoolVar(&o.logKeys, "log-keys", false, "print `KEY <mods+code>` per key press and `PASTE <text>` on Ctrl+V")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stderr)
			fs.Usage()
		}
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["item"] && set["corpus-id"] {
		return o, errors.New("--item and --corpus-id cannot be used together")
	}
	if set["advanced-item"] && set["advanced-corpus-id"] {
		return o, errors.New("--advanced-item and --advanced-corpus-id cannot be used together")
	}
	if !set["corpus-dir"] {
		o.corpusDir = getenv("RKL_CORPUS_DIR")
	}
	return o, nil
}

// parseSize parses `WxH` with non-zero sides.
func parseSize(s string) (w, h uint32, err error) {
	ws, hs, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0, errors.New("expected WxH")
	}
	wv, err := strconv.ParseUint(ws, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("width: %w", err)
	}
	hv, err := strconv.ParseUint(hs, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("height: %w", err)
	}
	if wv == 0 || hv == 0 {
		return 0, 0, errors.New("size must be non-zero")
	}
	return uint32(wv), uint32(hv), nil
}

// items are the two texts the game serves.
type items struct {
	normal, advanced string
}

func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading item text %s: %w", path, err)
	}
	return string(b), nil
}

func loadItems(o options) (items, error) {
	dir := o.corpusDir
	if dir == "" {
		dir = coretest.CorpusDir()
	}
	corpus := func(id string) (string, error) { return readText(filepath.Join(dir, id+".txt")) }
	var it items
	var err error
	switch {
	case o.item != "":
		it.normal, err = readText(o.item)
	case o.corpusID != "":
		it.normal, err = corpus(o.corpusID)
	default:
		err = errors.New("one of --item or --corpus-id is required")
	}
	if err != nil {
		return it, err
	}
	switch {
	case o.advancedItem != "":
		it.advanced, err = readText(o.advancedItem)
	case o.advancedCorpusID != "":
		it.advanced, err = corpus(o.advancedCorpusID)
	default:
		it.advanced = it.normal
	}
	return it, err
}

// mods are the modifiers held during a key event.
type mods struct{ ctrl, alt, shift, super bool }

// keyEvent is a key press or release, without auto-repeats.
type keyEvent struct {
	pressed bool
	// isC / isV: the key is C / V, physically or in the current layout. Virtual keyboards
	// (wtype, the app on Wayland) upload their own keymap, so the scancode is arbitrary there.
	isC, isV bool
	// code is the key's name for --log-keys (W3C KeyboardEvent.code names: KeyC, Digit1,
	// Enter, ...).
	code string
	mods mods
}

// forKey is the text to copy for a key event, if it is a copy chord. Super (Cmd) counts as
// Ctrl so the tool also behaves on macOS.
func (it items) forKey(ev keyEvent) (string, bool) {
	if !ev.isC || !(ev.mods.ctrl || ev.mods.super) {
		return "", false
	}
	if ev.mods.alt {
		return it.advanced, true
	}
	return it.normal, true
}

// clipboard is a backend's clipboard.
type clipboard interface {
	setText(text string) error
	text() (string, error)
}

// game is the backend-independent state: which chord copies what, and the output protocol.
type game struct {
	items   items
	logKeys bool
	debug   bool
	copies  int
	ready   bool
	// cDown: a copy-chord press was seen and its release is still pending.
	cDown bool
}

var outMu sync.Mutex

// say prints one protocol line.
func say(line string) {
	outMu.Lock()
	defer outMu.Unlock()
	_, _ = fmt.Fprintln(os.Stdout, line)
}

func (g *game) debugf(format string, args ...any) {
	if g.debug {
		fmt.Fprintf(os.Stderr, "fake-game: "+format+"\n", args...)
	}
}

// drawn reports a presented frame; the first one prints READY.
func (g *game) drawn() {
	if !g.ready {
		g.ready = true
		say("READY")
	}
}

// color is the fill colour (0xRRGGBB): dark brown, the colour of every PoE screenshot, with the
// copy count in the red channel so each copy repaints.
func (g *game) color() uint32 { return 0x201810 | uint32(g.copies&0xff)<<16 }

// key handles a key event; it reports whether the clipboard changed (the window should repaint).
func (g *game) key(ev keyEvent, cb clipboard) bool {
	g.debugf("key %+v", ev)
	if g.logKeys && ev.pressed {
		g.logKey(ev, cb)
	}
	text, ok := g.items.forKey(ev)
	if !ok {
		return false
	}
	// Copy on press. Virtual keyboards swap the keymap right before the press, and a toolkit
	// can drop that press; then copy on the orphan release.
	switch {
	case ev.pressed:
		g.cDown = true
	case g.cDown:
		g.cDown = false
		return false
	}
	if err := cb.setText(text); err != nil {
		fmt.Fprintf(os.Stderr, "fake-game: clipboard error: %v\n", err)
		return false
	}
	g.copies++
	say(fmt.Sprintf("COPIED %d", g.copies))
	return true
}

func (g *game) logKey(ev keyEvent, cb clipboard) {
	var name strings.Builder
	if ev.mods.ctrl {
		name.WriteString("ctrl+")
	}
	if ev.mods.alt {
		name.WriteString("alt+")
	}
	if ev.mods.shift {
		name.WriteString("shift+")
	}
	name.WriteString(ev.code)
	say("KEY " + name.String())
	if ev.isV && ev.mods.ctrl && !ev.mods.alt && !ev.mods.shift {
		text, err := cb.text()
		if err != nil {
			g.debugf("paste: %v", err)
		}
		say("PASTE " + text)
	}
}

// charCode is the W3C KeyboardEvent.code name for a letter or digit key, else "".
func charCode(r rune) string {
	switch {
	case r >= 'a' && r <= 'z':
		return "Key" + string(r-'a'+'A')
	case r >= 'A' && r <= 'Z':
		return "Key" + string(r)
	case r >= '0' && r <= '9':
		return "Digit" + string(r)
	}
	return ""
}

func main() {
	o, err := parseArgs(os.Args[1:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-game: %v (see -h)\n", err)
		os.Exit(2)
	}
	it, err := loadItems(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-game: %v\n", err)
		os.Exit(1)
	}
	g := &game{items: it, logKeys: o.logKeys, debug: os.Getenv("FAKE_GAME_DEBUG") == "1"}
	if o.hasExitAfter {
		time.AfterFunc(time.Duration(max(o.exitAfter, 0)*float64(time.Second)), func() {
			say("EXIT")
			os.Exit(0)
		})
	}
	err = run(g, o.width, o.height)
	say("EXIT")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-game: %v\n", err)
		os.Exit(1)
	}
}
