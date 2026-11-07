//go:build testhook

package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
)

// TestHookPort is the RKL_TEST_HOOK port, when set.
func TestHookPort() (int, bool) {
	p, err := strconv.Atoi(os.Getenv("RKL_TEST_HOOK"))
	return p, err == nil && p >= 0 && p <= 65535
}

// HookActions are what the hook drives outside the flow.
type HookActions struct {
	// Tray runs a tray item (`open_settings`, `open_diagnostics`, `price_check`, `quit`); false
	// when there is no tray.
	Tray func(item string) (ok bool, err error)
	// TrayPresent reports whether the tray icon exists.
	TrayPresent func() bool
	// Quit exits the app.
	Quit func()
}

// StartTestHook serves the e2e line protocol on 127.0.0.1:port (0 picks a port) and returns
// the bound port. One command per line, one reply line each.
func (f *Flow) StartTestHook(port int, actions HookActions) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, err
	}
	bound := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serveHook(conn, actions)
		}
	}()
	slog.Info("test hook listening", "addr", ln.Addr().String())
	return bound, nil
}

func (f *Flow) serveHook(conn net.Conn, actions HookActions) {
	defer conn.Close()
	var token *string
	if t, ok := os.LookupEnv(TestHookTokenEnv); ok {
		token = &t
	}
	authed := token == nil
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if IsHTTPRequest(line) {
			return
		}
		if !authed {
			if Authorised(line, token) {
				authed = true
				continue
			}
			fmt.Fprintln(conn, "err unauthorised")
			return
		}
		if _, err := fmt.Fprintln(conn, f.HandleHookLine(line, actions)); err != nil {
			return
		}
	}
}

// HandleHookLine runs one hook command and returns its reply.
func (f *Flow) HandleHookLine(line string, actions HookActions) string {
	st := f.st
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
		if _, _, err := f.HandleItemText(text, false); err != nil {
			return "err " + ipc.From(err).Error()
		}
		return "ok"
	case "state":
		return f.hookState(actions)
	case "settings":
		s := st.Settings()
		b, err := core.MarshalSerde(s)
		if err != nil {
			return "err " + err.Error()
		}
		return string(b)
	case "save_settings":
		var s config.Settings
		if err := json.Unmarshal([]byte(arg), &s); err != nil {
			return "err " + err.Error()
		}
		if _, err := f.ApplySettings(s); err != nil {
			return "err " + ipc.From(err).Error()
		}
		return "ok"
	case "tray":
		switch arg {
		case "open_settings", "open_diagnostics", "price_check", "quit":
		default:
			return fmt.Sprintf("err unknown tray item `%s`", arg)
		}
		if actions.Tray == nil {
			return "err tray is not available"
		}
		ok, err := actions.Tray(arg)
		if !ok {
			return "err tray is not available"
		}
		if err != nil {
			return "err " + err.Error()
		}
		return "ok"
	case "hide":
		if err := f.HideOverlay(); err != nil {
			return "err " + err.Error()
		}
		return "ok"
	case "quit":
		st.Quitting.Store(true)
		if actions.Quit != nil {
			go func() {
				time.Sleep(50 * time.Millisecond)
				actions.Quit()
			}()
		}
		return "ok"
	}
	return fmt.Sprintf("err unknown command `%s`", cmd)
}

func (f *Flow) hookState(actions HookActions) string {
	st := f.st
	var lastItem *string
	if last, ok := st.LastItem(); ok {
		lastItem = new(last.Item.BaseType)
	}
	st.mu.Lock()
	lastPrice := st.lastPrice
	st.mu.Unlock()
	card, height := st.Card()
	present := actions.TrayPresent != nil && actions.TrayPresent()
	layout := st.OverlayLayout()
	b, err := core.MarshalSerde(struct {
		OverlayVisible  bool               `json:"overlay_visible"`
		OverlayPinned   bool               `json:"overlay_pinned"`
		SettingsVisible bool               `json:"settings_visible"`
		TrayPresent     bool               `json:"tray_present"`
		LastItem        *string            `json:"last_item"`
		LastPrice       *core.PriceSummary `json:"last_price"`
		OverlayLayout   ipc.OverlayLayout  `json:"overlay_layout"`
		OverlayCard     [2]any             `json:"overlay_card"`
	}{
		OverlayVisible:  st.OverlayVisible.Load(),
		OverlayPinned:   st.IsPinned(),
		SettingsVisible: f.d.Settings().IsVisible(),
		TrayPresent:     present,
		LastItem:        lastItem,
		LastPrice:       lastPrice,
		OverlayLayout:   layout,
		OverlayCard:     [2]any{[2]int32{card.X, card.Y}, height},
	})
	if err != nil {
		return "err " + err.Error()
	}
	return string(b)
}
