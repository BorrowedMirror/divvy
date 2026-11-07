package x11

// A minimal raw X11 client for the RECORD data connection. EnableContext answers with an
// unbounded stream of replies that all carry the request's sequence number; xgb hands each
// sequence number exactly one reply, so the data connection speaks the wire protocol itself:
// connection setup, one EnableContext request, then a stream of replies.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

// displayAddr is a parsed DISPLAY.
type displayAddr struct {
	network, address string
	host             string // for Xauthority matching; "" = local
	number           string // display number
}

// parseDisplay parses DISPLAY the way xgb (and Xlib) do: `:1`, `:1.0`, `host:2`,
// `tcp/host:1`, `/path/to/socket:0` (launchd style).
func parseDisplay(display string) (displayAddr, error) {
	var d displayAddr
	orig := display
	colon := strings.LastIndex(display, ":")
	if colon < 0 {
		return d, fmt.Errorf("bad display string %q", orig)
	}
	var protocol, socket string
	if strings.HasPrefix(display, "/") {
		socket = display[:colon]
	} else if slash := strings.LastIndex(display[:colon], "/"); slash >= 0 {
		protocol = display[:slash]
		d.host = display[slash+1 : colon]
	} else {
		d.host = display[:colon]
	}
	rest := display[colon+1:]
	if dot := strings.LastIndex(rest, "."); dot >= 0 {
		rest = rest[:dot]
	}
	if n, err := strconv.Atoi(rest); err != nil || n < 0 {
		return d, fmt.Errorf("bad display string %q", orig)
	}
	d.number = rest
	switch {
	case socket != "":
		d.network, d.address = "unix", socket+":"+rest
	case d.host != "" && d.host != "unix":
		if protocol == "" {
			protocol = "tcp"
		}
		n, _ := strconv.Atoi(rest)
		d.network, d.address = protocol, net.JoinHostPort(d.host, strconv.Itoa(6000+n))
	default:
		d.host = ""
		d.network, d.address = "unix", "/tmp/.X11-unix/X"+rest
	}
	return d, nil
}

// Xauthority address families (X11/Xauth.h).
const (
	familyLocal = 256
	familyWild  = 65535
)

// findXauth returns the MIT-MAGIC-COOKIE-1 entry for display number number on host (local
// when empty) from an Xauthority stream; ok is false when none matches.
func findXauth(r io.Reader, host, number string) (name string, data []byte, ok bool) {
	if host == "" || host == "localhost" {
		host, _ = os.Hostname()
	}
	br := bufio.NewReader(r)
	readField := func() ([]byte, error) {
		var n uint16
		if err := binary.Read(br, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		b := make([]byte, n)
		_, err := io.ReadFull(br, b)
		return b, err
	}
	for {
		var family uint16
		if binary.Read(br, binary.BigEndian, &family) != nil {
			return "", nil, false
		}
		var fields [4][]byte
		for i := range fields {
			f, err := readField()
			if err != nil {
				return "", nil, false
			}
			fields[i] = f
		}
		addr, disp := string(fields[0]), string(fields[1])
		addrMatch := family == familyWild || (family == familyLocal && addr == host)
		if addrMatch && (disp == "" || disp == number) && string(fields[2]) == "MIT-MAGIC-COOKIE-1" {
			return string(fields[2]), fields[3], true
		}
	}
}

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

func pad4(n int) int { return (n + 3) &^ 3 }

// setupRequest is the little-endian connection setup request.
func setupRequest(authName string, authData []byte) []byte {
	buf := make([]byte, 12+pad4(len(authName))+pad4(len(authData)))
	buf[0] = 'l'
	binary.LittleEndian.PutUint16(buf[2:], 11)
	binary.LittleEndian.PutUint16(buf[6:], uint16(len(authName)))
	binary.LittleEndian.PutUint16(buf[8:], uint16(len(authData)))
	copy(buf[12:], authName)
	copy(buf[12+pad4(len(authName)):], authData)
	return buf
}

// readSetupReply consumes the setup reply, failing unless the server accepted us.
func readSetupReply(r io.Reader) error {
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return err
	}
	rest := make([]byte, int(binary.LittleEndian.Uint16(head[6:]))*4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return err
	}
	switch head[0] {
	case 1:
		return nil
	case 0:
		reason := rest[:min(int(head[1]), len(rest))]
		return fmt.Errorf("x11 connection refused: %s", reason)
	default:
		return errors.New("x11 connection needs further authentication")
	}
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

// RECORD reply categories.
const (
	recordFromServer  = 0
	recordStartOfData = 4
	recordEndOfData   = 5
)

// enableContextRequest is RECORD EnableContext (minor opcode 5).
func enableContextRequest(majorOpcode uint8, context uint32) []byte {
	buf := make([]byte, 8)
	buf[0], buf[1] = majorOpcode, 5
	binary.LittleEndian.PutUint16(buf[2:], 2)
	binary.LittleEndian.PutUint32(buf[4:], context)
	return buf
}

// readRecordReply reads one EnableContext reply: its category and data.
func readRecordReply(r io.Reader) (category uint8, data []byte, err error) {
	head := make([]byte, 32)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, nil, err
	}
	switch head[0] {
	case 1: // reply
	case 0:
		return 0, nil, fmt.Errorf("x11 error %d in RECORD stream", head[1])
	default: // an event: nothing selects any on this connection; skip it
		return 0xff, nil, nil
	}
	data = make([]byte, int(binary.LittleEndian.Uint32(head[4:]))*4)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}
	return head[1], data, nil
}
