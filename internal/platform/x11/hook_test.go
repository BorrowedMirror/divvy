package x11

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestMapsEvents(t *testing.T) {
	cases := []struct {
		typ, detail uint8
		x, y        int16
		want        *platform.InputEvent
	}{
		{evKeyPress, 38, 0, 0, &platform.InputEvent{Type: platform.KeyDown, Key: "KeyA"}},
		{evKeyRelease, 38, 0, 0, &platform.InputEvent{Type: platform.KeyUp, Key: "KeyA"}},
		{evKeyPress | 0x80, 38, 0, 0, &platform.InputEvent{Type: platform.KeyDown, Key: "KeyA"}},
		{evMotionNotify, 0, 10, -2, &platform.InputEvent{Type: platform.MouseMove, X: 10, Y: -2}},
		{evButtonPress, 5, 0, 0, &platform.InputEvent{Type: platform.Wheel, Delta: -1}},
		{evButtonPress, 4, 0, 0, &platform.InputEvent{Type: platform.Wheel, Delta: 1}},
		{evButtonRelease, 4, 0, 0, nil},
		{evButtonPress, 1, 0, 0, nil},
		{12, 0, 0, 0, nil},
	}
	for _, c := range cases {
		got, ok := mapEvent(c.typ, c.detail, c.x, c.y)
		if c.want == nil {
			assert.False(t, ok, "%d/%d: unexpected %v", c.typ, c.detail, got)
			continue
		}
		assert.True(t, ok, "%d/%d", c.typ, c.detail)
		assert.Equal(t, *c.want, got, "%d/%d", c.typ, c.detail)
	}
}

func TestRecordEventsDecodesWireEvents(t *testing.T) {
	ev := func(typ, detail uint8, x, y int16) []byte {
		b := make([]byte, 32)
		b[0], b[1] = typ, detail
		binary.LittleEndian.PutUint16(b[20:], uint16(x))
		binary.LittleEndian.PutUint16(b[22:], uint16(y))
		return b
	}
	var data []byte
	data = append(data, ev(evKeyPress, 71, 0, 0)...)
	data = append(data, ev(evButtonPress, 1, 0, 0)...)
	data = append(data, ev(evMotionNotify, 0, 300, -5)...)
	data = append(data, 1, 2, 3) // trailing garbage is ignored
	var got []platform.InputEvent
	recordEvents(data, func(e platform.InputEvent) { got = append(got, e) })
	assert.Equal(t, []platform.InputEvent{{Type: platform.KeyDown, Key: "F5"}, {Type: platform.MouseMove, X: 300, Y: -5}}, got)
	assert.Equal(t, uint8(evKeyRelease), recordRangeLast(false))
	assert.Equal(t, uint8(evMotionNotify), recordRangeLast(true))
}
