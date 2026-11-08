package x11

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "nocolon", ":", ":x", "host:"} {
		_, err := parseDisplay(bad)
		require.Error(t, err, bad)
	}
}

func TestSetupAndRecordWire(t *testing.T) {
	req := setupRequest("MIT-MAGIC-COOKIE-1", make([]byte, 16))
	assert.Len(t, req, 12+20+16)
	assert.Equal(t, byte('l'), req[0])
	assert.Equal(t, uint16(18), binary.LittleEndian.Uint16(req[6:]))

	require.NoError(t, readSetupReply(bytes.NewReader([]byte{1, 0, 11, 0, 0, 0, 1, 0, 9, 9, 9, 9})))
	require.EqualError(t, readSetupReply(bytes.NewReader([]byte{0, 2, 11, 0, 0, 0, 1, 0, 'n', 'o', 0, 0})), "x11 connection refused: no")
	assert.Equal(t, []byte{146, 5, 2, 0, 1, 0, 0x40, 0}, enableContextRequest(146, 0x400001))

	reply := make([]byte, 32+32)
	reply[0], reply[1] = 1, recordFromServer
	binary.LittleEndian.PutUint32(reply[4:], 8)
	reply[32] = evKeyPress
	cat, data, err := readRecordReply(bytes.NewReader(reply))
	require.NoError(t, err)
	assert.Equal(t, uint8(recordFromServer), cat)
	assert.Len(t, data, 32)
	assert.Equal(t, byte(evKeyPress), data[0])

	_, _, err = readRecordReply(bytes.NewReader(make([]byte, 32)))
	require.Error(t, err, "x11 error")
}
