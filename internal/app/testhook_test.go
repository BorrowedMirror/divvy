//go:build testhook

package app

import (
	"bufio"
	"encoding/json"
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
