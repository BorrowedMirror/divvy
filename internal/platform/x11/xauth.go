package x11

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
)

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
