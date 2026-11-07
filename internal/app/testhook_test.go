//go:build testhook

package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/testkit"
)

func TestHookDrivesTheFlowOverTCP(t *testing.T) {
	a, d, _ := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	t.Setenv(TestHookTokenEnv, "s3cret")
	port, err := a.StartTestHook(0, HookActions{})
	require.NoError(t, err)
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	defer conn.Close()
	r := bufio.NewReader(conn)
	ask := func(line string) string {
		_, err := fmt.Fprintln(conn, line)
		require.NoError(t, err)
		reply, err := r.ReadString('\n')
		require.NoError(t, err)
		return reply[:len(reply)-1]
	}

	_, err = fmt.Fprintln(conn, "auth s3cret")
	require.NoError(t, err)
	text, _ := json.Marshal(corpusText(t, "gloves/rare-stocky-mitts"))
	assert.Equal(t, "ok", ask("text "+string(text)))
	assert.True(t, d.overlay.IsVisible())

	var state map[string]any
	require.NoError(t, json.Unmarshal([]byte(ask("state")), &state))
	assert.Equal(t, true, state["overlay_visible"])
	assert.Equal(t, "Stocky Mitts", state["last_item"])

	assert.Equal(t, "ok", ask("hide"))
	assert.False(t, d.overlay.IsVisible())
	assert.Equal(t, "err tray is not available", ask("tray quit"))
	assert.Equal(t, "err unknown command `nope`", ask("nope"))
}

func TestHookRefusesWrongTokens(t *testing.T) {
	a, _, _ := newTestApp(t, testkit.NewFakePlatformBuilder())
	t.Setenv(TestHookTokenEnv, "s3cret")
	port, err := a.StartTestHook(0, HookActions{})
	require.NoError(t, err)
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	defer conn.Close()
	fmt.Fprintln(conn, "state")
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	assert.Equal(t, "err unauthorised\n", reply)
}

func TestHookForwardsDOMQueries(t *testing.T) {
	a, _, _ := newTestApp(t, testkit.NewFakePlatformBuilder())
	type query struct{ window, selector string }
	var got []query
	actions := HookActions{DOM: func(window, selector string) (string, error) {
		got = append(got, query{window, selector})
		if window == "settings" {
			return "", errors.New("no reply from the settings window within 5s")
		}
		return `{"exists":true,"visible":true,"text":"2 div"}`, nil
	}}

	assert.Equal(t, `{"exists":true,"visible":true,"text":"2 div"}`,
		a.HandleHookLine(`dom overlay [data-testid="price-median"]`, actions))
	// The selector is the rest of the line, spaces included.
	a.HandleHookLine(`dom overlay  article  > dl dd `, actions)
	assert.Equal(t, "err no reply from the settings window within 5s",
		a.HandleHookLine(`dom settings [data-testid="settings-root"]`, actions))
	assert.Equal(t, []query{
		{"overlay", `[data-testid="price-median"]`},
		{"overlay", "article  > dl dd"},
		{"settings", `[data-testid="settings-root"]`},
	}, got)

	usage := "err usage: dom <overlay|settings> <css selector>"
	for _, line := range []string{"dom", "dom overlay", "dom overlay   ", "dom diagnostics body", "dom  body"} {
		assert.Equal(t, usage, a.HandleHookLine(line, actions), line)
	}
	assert.Len(t, got, 3, "malformed lines must not reach the web view")
	assert.Equal(t, "err dom is not available", a.HandleHookLine("dom overlay body", HookActions{}))
}
