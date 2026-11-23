//go:build testhook

package app

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/ipc"
)

// The e2e test hook: a line protocol on 127.0.0.1 that drives the flow and reports its state
// (CONTRACTS.md#test-hook). Only test-hook builds have it.

// testHookTokenEnv optionally requires `auth <token>` as the first line of each connection.
const testHookTokenEnv = "DIVVY_TEST_HOOK_TOKEN"

// HookActions are what the hook drives outside the flow.
type HookActions struct {
	// Tray runs a tray item (`open_settings`, `open_diagnostics`, `price_check`, `quit`); false
	// when there is no tray.
	Tray func(item string) (ok bool, err error)
	// TrayPresent reports whether the tray icon exists.
	TrayPresent func() bool
	// Quit exits the app.
	Quit func()
	// DOM asks the web view of window (`overlay`, `notice` or `settings`) about the first element
	// matching a CSS selector and returns its JSON reply,
	// `{"exists":bool,"visible":bool,"text":string}`. Read-only.
	DOM func(ctx context.Context, window, selector string) (string, error)
}

// domWindows are the windows `dom` can query.
var domWindows = []string{"overlay", "notice", "settings"}

// StartTestHook serves the e2e line protocol on 127.0.0.1:port (0 picks a port) until ctx is
// done, and returns the bound port. One command per line, one reply line each.
func (f *Flow) StartTestHook(ctx context.Context, port int, actions HookActions) (int, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	context.AfterFunc(ctx, func() { ln.Close() })
	addr, _ := ln.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serveHook(ctx, conn, actions)
		}
	}()
	slog.Info("test hook listening", "addr", addr.String())
	return addr.Port, nil
}

func (f *Flow) serveHook(ctx context.Context, conn net.Conn, actions HookActions) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	token, required := os.LookupEnv(testHookTokenEnv)
	authed := !required
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if isHTTPRequest(line) {
			return
		}
		if !authed {
			if authorised(line, token) {
				authed = true
				continue
			}
			fmt.Fprintln(conn, "err unauthorised")
			return
		}
		if _, err := fmt.Fprintln(conn, f.handleHookLine(ctx, line, actions)); err != nil {
			return
		}
	}
}

// isHTTPRequest reports a line that starts an HTTP request: the hook speaks a line protocol on
// 127.0.0.1, and a browser page must not be able to drive it with a fetch.
func isHTTPRequest(line string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	return slices.ContainsFunc([]string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"},
		func(m string) bool { return strings.EqualFold(first, m) })
}

// authorised reports whether line (`auth <token>`) unlocks a connection that requires token. An
// empty token unlocks nothing.
func authorised(line, token string) bool {
	got, ok := strings.CutPrefix(line, "auth ")
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) == 1
}

// handleHookLine runs one hook command and returns its reply.
func (f *Flow) handleHookLine(ctx context.Context, line string, actions HookActions) string {
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch cmd {
	case "trigger":
		f.Trigger(arg == "1")
		return "ok"
	case "text":
		var text string
		if err := json.Unmarshal([]byte(arg), &text); err != nil {
			return "err " + err.Error()
		}
		if _, _, err := f.handleItemText(text, false); err != nil {
			return "err " + ipc.From(err).Error()
		}
		return "ok"
	case "state":
		return f.hookState(actions)
	case "settings":
		b, err := json.Marshal(f.st.Settings(), json.Deterministic(true))
		if err != nil {
			return "err " + err.Error()
		}
		return string(b)
	case "save_settings":
		var s config.Settings
		if err := json.Unmarshal([]byte(arg), &s); err != nil {
			return "err " + err.Error()
		}
		if _, err := f.applySettings(s); err != nil {
			return "err " + ipc.From(err).Error()
		}
		return "ok"
	case "tray":
		return hookTray(arg, actions)
	case "dom":
		return hookDOM(ctx, arg, actions)
	case "hide":
		f.hideOverlay()
		return "ok"
	case "quit":
		if actions.Quit != nil {
			// After the reply has gone out.
			time.AfterFunc(50*time.Millisecond, actions.Quit)
		}
		return "ok"
	}
	return fmt.Sprintf("err unknown command `%s`", cmd)
}

// hookTray runs `tray <item>`.
func hookTray(item string, actions HookActions) string {
	switch item {
	case "open_settings", "open_diagnostics", "price_check", "quit":
	default:
		return fmt.Sprintf("err unknown tray item `%s`", item)
	}
	if actions.Tray == nil {
		return "err tray is not available"
	}
	ok, err := actions.Tray(item)
	switch {
	case !ok:
		return "err tray is not available"
	case err != nil:
		return "err " + err.Error()
	}
	return "ok"
}

// hookDOM runs `dom <window> <css selector>`; the selector is the rest of the line.
func hookDOM(ctx context.Context, arg string, actions HookActions) string {
	window, selector, _ := strings.Cut(arg, " ")
	selector = strings.TrimSpace(selector)
	if !slices.Contains(domWindows, window) || selector == "" {
		return "err usage: dom <" + strings.Join(domWindows, "|") + "> <css selector>"
	}
	if actions.DOM == nil {
		return "err dom is not available"
	}
	reply, err := actions.DOM(ctx, window, selector)
	if err != nil {
		return "err " + err.Error()
	}
	return reply
}

func (f *Flow) hookState(actions HookActions) string {
	st := f.st
	var lastItem *string
	if last, ok := st.lastItem(); ok {
		lastItem = &last.item.BaseType
	}
	card, height := st.cardPosition()
	b, err := json.Marshal(struct {
		OverlayVisible  bool               `json:"overlay_visible"`
		OverlayPinned   bool               `json:"overlay_pinned"`
		SettingsVisible bool               `json:"settings_visible"`
		NoticeVisible   bool               `json:"notice_visible"`
		TrayPresent     bool               `json:"tray_present"`
		CanCopy         bool               `json:"can_copy"`
		LastItem        *string            `json:"last_item"`
		LastPrice       *core.PriceSummary `json:"last_price"`
		OverlayLayout   ipc.OverlayLayout  `json:"overlay_layout"`
		OverlayCard     [2]any             `json:"overlay_card"`
	}{
		OverlayVisible:  st.overlayVisible.Load(),
		OverlayPinned:   st.pinned.Load(),
		SettingsVisible: f.d.Settings().IsVisible(),
		NoticeVisible:   f.d.Notice().IsVisible(),
		TrayPresent:     actions.TrayPresent != nil && actions.TrayPresent(),
		CanCopy:         st.platform != nil && mayCopy(st.platform.Capabilities.WindowTracking, st.platform.WindowTracker.IsGameFocused),
		LastItem:        lastItem,
		LastPrice:       st.lastPriceSummary(),
		OverlayLayout:   st.layout(),
		OverlayCard:     [2]any{[2]int32{card.X, card.Y}, height},
	})
	if err != nil {
		return "err " + err.Error()
	}
	return string(b)
}
