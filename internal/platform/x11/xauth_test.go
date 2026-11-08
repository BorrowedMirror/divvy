package x11

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	assert.True(t, ok)
	assert.Equal(t, "MIT-MAGIC-COOKIE-1", name)
	assert.Equal(t, cookie, data)

	_, _, ok = findXauth(bytes.NewReader(file), "", "5")
	assert.False(t, ok, "no entry for :5")
	wild := xauthEntry(familyWild, "", "", "MIT-MAGIC-COOKIE-1", cookie)
	_, _, ok = findXauth(bytes.NewReader(wild), "", "9")
	assert.True(t, ok, "wildcard entry")
}
