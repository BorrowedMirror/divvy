package x11

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// dialTimeout bounds connecting to the X server and its setup handshake.
const dialTimeout = 5 * time.Second

// dialRaw opens and sets up a raw connection to display ("" = $DISPLAY).
func dialRaw(display string) (net.Conn, error) {
	if display == "" {
		display = os.Getenv("DISPLAY")
	}
	d, err := parseDisplay(display)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(ctx, d.network, d.address)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", display, err)
	}
	// The handshake has the same deadline; the reply stream afterwards has none.
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	name, data := xauthority(d)
	if _, err := conn.Write(setupRequest(name, data)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readSetupReply(conn); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// xauthority is the MIT-MAGIC-COOKIE-1 entry for d from $XAUTHORITY or ~/.Xauthority; empty when
// there is none.
func xauthority(d displayAddr) (string, []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil
		}
		path = filepath.Join(home, ".Xauthority")
	}
	f, err := os.Open(path) //nolint:gosec // $XAUTHORITY, as every X client reads it
	if err != nil {
		return "", nil
	}
	defer f.Close()
	name, data, ok := findXauth(f, d.host, d.number)
	if !ok {
		return "", nil
	}
	return name, data
}
