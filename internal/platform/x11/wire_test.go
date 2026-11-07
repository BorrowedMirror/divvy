package x11

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestParseDisplay(t *testing.T) {
	cases := map[string]displayAddr{
		":1":                {network: "unix", address: "/tmp/.X11-unix/X1", number: "1"},
		":0.0":              {network: "unix", address: "/tmp/.X11-unix/X0", number: "0"},
		"unix:3":            {network: "unix", address: "/tmp/.X11-unix/X3", number: "3"},
		"host:2.1":          {network: "tcp", address: "host:6002", host: "host", number: "2"},
		"tcp/host:1":        {network: "tcp", address: "host:6001", host: "host", number: "1"},
		"/tmp/launch-12/:0": {network: "unix", address: "/tmp/launch-12/:0", number: "0"},
	}
	for in, want := range cases {
		got, err := parseDisplay(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "nocolon", ":", ":x", "host:"} {
		if _, err := parseDisplay(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func xauthEntry(family uint16, addr, disp, name string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, family)
	for _, f := range [][]byte{[]byte(addr), []byte(disp), []byte(name), data} {
		_ = binary.Write(&b, binary.BigEndian, uint16(len(f)))
		b.Write(f)
	}
	return b.Bytes()
}

func TestFindXauthMatchesHostAndDisplay(t *testing.T) {
	host, _ := os.Hostname()
	cookie := bytes.Repeat([]byte{7}, 16)
	var file []byte
	file = append(file, xauthEntry(familyLocal, "otherhost", "1", "MIT-MAGIC-COOKIE-1", []byte("x"))...)
	file = append(file, xauthEntry(familyLocal, host, "0", "MIT-MAGIC-COOKIE-1", []byte("zero"))...)
	file = append(file, xauthEntry(familyLocal, host, "1", "MIT-MAGIC-COOKIE-1", cookie)...)
	name, data, ok := findXauth(bytes.NewReader(file), "", "1")
	if !ok || name != "MIT-MAGIC-COOKIE-1" || !bytes.Equal(data, cookie) {
		t.Fatalf("got %q %v %v", name, data, ok)
	}
	if _, _, ok := findXauth(bytes.NewReader(file), "", "5"); ok {
		t.Fatal("no entry for :5")
	}
	wild := xauthEntry(familyWild, "", "", "MIT-MAGIC-COOKIE-1", cookie)
	if _, _, ok := findXauth(bytes.NewReader(wild), "", "9"); !ok {
		t.Fatal("wildcard entry")
	}
}

func TestSetupAndRecordWire(t *testing.T) {
	req := setupRequest("MIT-MAGIC-COOKIE-1", make([]byte, 16))
	if len(req) != 12+20+16 || req[0] != 'l' || binary.LittleEndian.Uint16(req[6:]) != 18 {
		t.Fatalf("setup request %v", req[:12])
	}
	if err := readSetupReply(bytes.NewReader([]byte{1, 0, 11, 0, 0, 0, 1, 0, 9, 9, 9, 9})); err != nil {
		t.Fatal(err)
	}
	if err := readSetupReply(bytes.NewReader([]byte{0, 2, 11, 0, 0, 0, 1, 0, 'n', 'o', 0, 0})); err == nil || err.Error() != "x11 connection refused: no" {
		t.Fatalf("refused: %v", err)
	}
	if got := enableContextRequest(146, 0x400001); !bytes.Equal(got, []byte{146, 5, 2, 0, 1, 0, 0x40, 0}) {
		t.Fatalf("enable context %v", got)
	}
	reply := make([]byte, 32+32)
	reply[0], reply[1] = 1, recordFromServer
	binary.LittleEndian.PutUint32(reply[4:], 8)
	reply[32] = evKeyPress
	cat, data, err := readRecordReply(bytes.NewReader(reply))
	if err != nil || cat != recordFromServer || len(data) != 32 || data[0] != evKeyPress {
		t.Fatalf("reply %d %v %v", cat, data, err)
	}
	if _, _, err := readRecordReply(bytes.NewReader(make([]byte, 32))); err == nil {
		t.Fatal("x11 error")
	}
}
