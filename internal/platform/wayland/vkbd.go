//go:build linux

package wayland

import (
	"log/slog"
	"sync"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// virtualKeyboard injects chords through zwp_virtual_keyboard_v1.
//
// The connection is opened lazily and kept on success: on sway the virtual keyboard can fail
// right after a compositor restart, so every SendChord retries until it works (and a connection
// that breaks later is dropped and reopened on the next send). While it cannot be opened, a
// fallback (ydotool), when set, sends the chord instead. Wayland cannot tell which modifiers are
// held, so every chord modifier is pressed and released ([chordSteps]).
type virtualKeyboard struct {
	display  string // "" = $WAYLAND_DISPLAY
	fallback platform.InputSimulator

	mu   sync.Mutex
	conn *vkConn
}

// ensureReady opens the virtual keyboard now if it is not yet open.
func (v *virtualKeyboard) ensureReady() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := v.open()
	return err
}

func (v *virtualKeyboard) open() (*vkConn, error) {
	if v.conn == nil {
		c, err := openVirtualKeyboard(v.display)
		if err != nil {
			return nil, err
		}
		v.conn = c
	}
	return v.conn, nil
}

// ready reports whether the virtual keyboard is open.
func (v *virtualKeyboard) ready() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.conn != nil
}

// SendChord implements [platform.InputSimulator].
func (v *virtualKeyboard) SendChord(chord platform.Chord) error {
	steps, err := chordSteps(chord)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	conn, err := v.open()
	if err != nil {
		if v.fallback == nil {
			return err
		}
		slog.Debug("virtual keyboard unavailable; using fallback simulator", "error", err)
		return v.fallback.SendChord(chord)
	}
	for _, s := range steps {
		if err = conn.key(s.code, s.down); err != nil {
			break
		}
	}
	if err == nil {
		err = conn.sync()
	}
	if err != nil {
		conn.close()
		v.conn = nil
	}
	return err
}
