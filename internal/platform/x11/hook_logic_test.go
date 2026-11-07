package x11

import (
	"reflect"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
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
			if ok {
				t.Errorf("%d/%d: unexpected %v", c.typ, c.detail, got)
			}
			continue
		}
		if !ok || got != *c.want {
			t.Errorf("%d/%d: got %v %v, want %v", c.typ, c.detail, got, ok, *c.want)
		}
	}
}

func TestRecordEventsDecodesWireEvents(t *testing.T) {
	ev := func(typ, detail uint8, x, y int16) []byte {
		b := make([]byte, 32)
		b[0], b[1] = typ, detail
		b[20], b[21] = byte(uint16(x)), byte(uint16(x)>>8)
		b[22], b[23] = byte(uint16(y)), byte(uint16(y)>>8)
		return b
	}
	var data []byte
	data = append(data, ev(evKeyPress, 71, 0, 0)...)
	data = append(data, ev(evButtonPress, 1, 0, 0)...)
	data = append(data, ev(evMotionNotify, 0, 300, -5)...)
	data = append(data, 1, 2, 3) // trailing garbage is ignored
	var got []platform.InputEvent
	recordEvents(data, func(e platform.InputEvent) { got = append(got, e) })
	want := []platform.InputEvent{{Type: platform.KeyDown, Key: "F5"}, {Type: platform.MouseMove, X: 300, Y: -5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if recordRangeLast(false) != evKeyRelease || recordRangeLast(true) != evMotionNotify {
		t.Fatal("record range")
	}
}

func TestForwardNeverBlocks(t *testing.T) {
	ch := make(chan platform.InputEvent, 1)
	if !forward(ch, platform.InputEvent{Type: platform.Wheel, Delta: 1}) {
		t.Fatal("first send")
	}
	if forward(ch, platform.InputEvent{Type: platform.Wheel, Delta: 2}) {
		t.Fatal("full channel drops")
	}
}
