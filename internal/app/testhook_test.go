//go:build testhook

package app

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform/platformtest"
)

// dialHook connects to the hook on port and returns a function that sends one line and reads
// the reply.
func dialHook(t *testing.T, port int) (net.Conn, func(line string) string) {
	t.Helper()
	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	r := bufio.NewReader(conn)
	return conn, func(line string) string {
		t.Helper()
		_, err := fmt.Fprintln(conn, line)
		require.NoError(t, err)
		reply, err := r.ReadString('\n')
		require.NoError(t, err)
		return strings.TrimSuffix(reply, "\n")
	}
}

func TestHookDrivesTheFlowOverTCP(t *testing.T) {
	a, d, _ := newTestApp(t, platformtest.Options{Window: game()})
	t.Setenv(testHookTokenEnv, "s3cret")
	port, err := a.StartTestHook(t.Context(), 0, HookActions{})
	require.NoError(t, err)
	conn, ask := dialHook(t, port)

	_, err = fmt.Fprintln(conn, "auth s3cret")
	require.NoError(t, err)
	text, err := json.Marshal(corpusText(t, "gloves/rare-stocky-mitts"))
	require.NoError(t, err)
	assert.Equal(t, "ok", ask("text "+string(text)))
	assert.True(t, d.overlay.IsVisible())

	var state map[string]any
	require.NoError(t, json.Unmarshal([]byte(ask("state")), &state))
	assert.Equal(t, true, state["overlay_visible"])
	assert.Equal(t, "Stocky Mitts", state["last_item"])
	assert.Nil(t, state["last_price"])

	assert.Equal(t, "ok", ask("hide"))
	assert.False(t, d.overlay.IsVisible())
	assert.Equal(t, "err tray is not available", ask("tray quit"))
	assert.Equal(t, "err unknown tray item `nope`", ask("tray nope"))
	assert.Equal(t, "err unknown command `nope`", ask("nope"))
}

func TestHookRefusesWrongTokensAndHTTP(t *testing.T) {
	a, _, _ := newTestApp(t, platformtest.Options{})
	t.Setenv(testHookTokenEnv, "s3cret")
	port, err := a.StartTestHook(t.Context(), 0, HookActions{})
	require.NoError(t, err)
	_, ask := dialHook(t, port)
	assert.Equal(t, "err unauthorised", ask("auth wrong"))

	conn, _ := dialHook(t, port)
	fmt.Fprintln(conn, "GET / HTTP/1.1")
	_, err = bufio.NewReader(conn).ReadString('\n')
	assert.Error(t, err, "an HTTP request is dropped without a reply")
}

func TestHookStopsWithItsContext(t *testing.T) {
	a, _, _ := newTestApp(t, platformtest.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	port, err := a.StartTestHook(ctx, 0, HookActions{})
	require.NoError(t, err)
	conn, ask := dialHook(t, port)
	assert.Equal(t, "err unknown command `x`", ask("x"))
	cancel()
	require.Eventually(t, func() bool {
		var d net.Dialer
		c, err := d.DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			c.Close()
		}
		return err != nil
	}, 5*time.Second, 10*time.Millisecond, "the listener is closed")
	_, err = bufio.NewReader(conn).ReadString('\n')
	assert.Error(t, err, "open connections are closed")
}

func TestHookTokenAuth(t *testing.T) {
	assert.True(t, authorised("auth s3cret", "s3cret"))
	assert.True(t, authorised("auth  s3cret ", "s3cret"))
	assert.False(t, authorised("auth wrong", "s3cret"))
	assert.False(t, authorised("state", "s3cret"))
	assert.False(t, authorised("auth ", ""), "an empty token unlocks nothing")
}

func TestHookRejectsHTTPRequests(t *testing.T) {
	assert.True(t, isHTTPRequest("GET / HTTP/1.1"))
	assert.True(t, isHTTPRequest("post /x HTTP/1.1"))
	assert.True(t, isHTTPRequest("OPTIONS * HTTP/1.1"))
	assert.False(t, isHTTPRequest("trigger 1"))
	assert.False(t, isHTTPRequest("state"))
	assert.False(t, isHTTPRequest(""))
}

func TestHookForwardsDOMQueries(t *testing.T) {
	a, _, _ := newTestApp(t, platformtest.Options{})
	type query struct{ window, selector string }
	var got []query
	actions := HookActions{DOM: func(_ context.Context, window, selector string) (string, error) {
		got = append(got, query{window, selector})
		if window == "settings" {
			return "", errors.New("no reply from the settings window within 5s")
		}
		return `{"exists":true,"visible":true,"text":"2 div"}`, nil
	}}
	run := func(line string, actions HookActions) string { return a.handleHookLine(t.Context(), line, actions) }

	assert.JSONEq(t, `{"exists":true,"visible":true,"text":"2 div"}`, run(`dom overlay [data-testid="price-median"]`, actions))
	// The selector is the rest of the line, spaces included.
	run(`dom overlay  article  > dl dd `, actions)
	assert.Equal(t, "err no reply from the settings window within 5s", run(`dom settings [data-testid="settings-root"]`, actions))
	assert.Equal(t, []query{
		{"overlay", `[data-testid="price-median"]`},
		{"overlay", "article  > dl dd"},
		{"settings", `[data-testid="settings-root"]`},
	}, got)

	usage := "err usage: dom <overlay|settings> <css selector>"
	for _, line := range []string{"dom", "dom overlay", "dom overlay   ", "dom diagnostics body", "dom  body"} {
		assert.Equal(t, usage, run(line, actions), line)
	}
	assert.Len(t, got, 3, "malformed lines must not reach the web view")
	assert.Equal(t, "err dom is not available", run("dom overlay body", HookActions{}))
}

// fakeBus delivers emitted queries to a goroutine that plays the web view.
type fakeBus struct {
	mu      sync.Mutex
	queries []domQuery
	answer  func(q domQuery)
}

func (b *fakeBus) emit(event string, payload any) {
	q, ok := payload.(domQuery)
	if event != DOMQueryEvent || !ok {
		return
	}
	b.mu.Lock()
	b.queries = append(b.queries, q)
	b.mu.Unlock()
	if b.answer != nil {
		go b.answer(q)
	}
}

func TestDOMBridgeMatchesRepliesByID(t *testing.T) {
	bus := &fakeBus{}
	bridge := NewDOMBridge(bus.emit, time.Second)
	bus.answer = func(q domQuery) {
		bridge.OnReply(map[string]any{"id": "nope", "exists": false}) // someone else's reply
		bridge.OnReply(map[string]any{"id": q.ID, "exists": true, "visible": q.Window == "overlay", "text": q.Selector})
	}
	reply, err := bridge.Query(t.Context(), "overlay", "main h1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"exists":true,"visible":true,"text":"main h1"}`, reply)
	require.Len(t, bus.queries, 1)
	assert.Equal(t, domQuery{ID: bus.queries[0].ID, Window: "overlay", Selector: "main h1"}, bus.queries[0])

	bus.answer = nil
	bridge = NewDOMBridge(bus.emit, 20*time.Millisecond)
	_, err = bridge.Query(t.Context(), "settings", "body")
	require.EqualError(t, err, "no reply from the settings window within 20ms (not loaded yet?)")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = NewDOMBridge(bus.emit, time.Minute).Query(ctx, "settings", "body")
	require.ErrorIs(t, err, context.Canceled)
}
