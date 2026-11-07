//go:build linux

package x11

import (
	"fmt"
	"net"
	"os"
)

func xauthority(d displayAddr) (string, []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil
		}
		path = home + "/.Xauthority"
	}
	f, err := os.Open(path)
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

// dialRaw opens and sets up a raw connection to display ("" = $DISPLAY).
func dialRaw(display string) (net.Conn, error) {
	if display == "" {
		display = os.Getenv("DISPLAY")
	}
	d, err := parseDisplay(display)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(d.network, d.address)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", display, err)
	}
	name, data := xauthority(d)
	if _, err := conn.Write(setupRequest(name, data)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readSetupReply(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
