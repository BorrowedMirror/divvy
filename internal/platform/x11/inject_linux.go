package x11

// Keystroke injection through XTEST.
//
// Before a chord the simulator (1) waits until the triggering hotkey's key is physically
// released, because the hotkey's XGrabKey grab would otherwise swallow the synthetic chord, and
// (2) leaves held chord modifiers alone and releases extra ones ([platform.RunChord]).
//
// The XTEST connection is opened lazily and cached on success; every SendChord retries after a
// failure (and drops a broken connection). When it cannot be opened and a fallback is set, the
// chord goes through the fallback instead.

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// releaseWait is how long the simulator waits for the hotkey key to come up before sending
// anyway.
const releaseWait = 400 * time.Millisecond

// simulator is the X11 [platform.InputSimulator].
type simulator struct {
	display string
	// fallback, when set, sends the chord whenever the X connection cannot be opened.
	fallback platform.InputSimulator

	mu   sync.Mutex
	conn *xConn
}

var _ platform.InputSimulator = (*simulator)(nil)

// newSimulator never fails: the connection is opened on first use (see
// [simulator.ensureReady]).
func newSimulator(display string) *simulator {
	return &simulator{display: display}
}

// ensureReady opens the XTEST connection now if it is not yet (cached on success; retried on
// the next call after a failure).
func (s *simulator) ensureReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.connLocked()
	return err
}

// ready reports whether the connection is open.
func (s *simulator) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

func (s *simulator) connLocked() (*xConn, error) {
	if s.conn != nil {
		return s.conn, nil
	}
	conn, err := connect(s.display, nil)
	if err != nil {
		return nil, err
	}
	if err := conn.useExtension("XTEST"); err != nil {
		conn.close()
		return nil, err
	}
	s.conn = conn
	return conn, nil
}

// SendChord implements [platform.InputSimulator].
func (s *simulator) SendChord(chord platform.Chord) error {
	sym, ok := chordKeysym(chord.Key)
	if !ok {
		return platform.InvalidChord(chord.String())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, err := s.connLocked()
	if err != nil {
		if s.fallback == nil {
			return err
		}
		slog.Debug("xtest unavailable; using fallback simulator", "error", err)
		return s.fallback.SendChord(chord)
	}
	err = sendChord(conn, chord, sym)
	if err != nil && !conn.alive() {
		conn.close()
		s.conn = nil
	}
	return err
}

func sendChord(conn *xConn, chord platform.Chord, sym uint32) error {
	km, err := keyboardMapping(conn)
	if err != nil {
		return err
	}
	key, ok := km.keycode(sym)
	if !ok {
		return platform.Backend("x11: no keycode for %s in the keyboard map", chord.Key)
	}
	held, ok := settle(conn, releaseWait)
	if !ok {
		held = heldState{} // the server cannot be queried: treat as nothing held
	}
	keys := &xtestKeys{conn: conn, km: km, key: key, held: held}
	err = platform.RunChord(chord.Modifiers, held.modifiers, keys)
	// Round trip so the server has processed every fake event before we return.
	if _, serr := xproto.GetInputFocus(conn.c).Reply(); err == nil && serr != nil {
		err = xErr(serr)
	}
	return err
}

// kbMap is a GetKeyboardMapping reply.
type kbMap struct {
	min  uint8
	per  int
	syms []uint32
}

func keyboardMapping(conn *xConn) (kbMap, error) {
	setup := conn.setup()
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	r, err := xproto.GetKeyboardMapping(conn.c, setup.MinKeycode, byte(count)).Reply()
	if err == nil && r == nil {
		err = errors.New("no reply")
	}
	if err != nil {
		return kbMap{}, xErr(fmt.Errorf("keyboard mapping: %w", err))
	}
	syms := make([]uint32, len(r.Keysyms))
	for i, k := range r.Keysyms {
		syms[i] = uint32(k)
	}
	return kbMap{min: uint8(setup.MinKeycode), per: int(r.KeysymsPerKeycode), syms: syms}, nil
}

func (k kbMap) keycode(sym uint32) (uint8, bool) {
	return keysymToKeycode(k.min, k.per, k.syms, sym)
}

func (x *xConn) setup() *xproto.SetupInfo { return xproto.Setup(x.c) }

// settle waits (up to limit) for the hotkey's non-modifier keys to come up, then reports the
// held modifiers; ok is false when the server cannot be queried.
func settle(conn *xConn, limit time.Duration) (heldState, bool) {
	mm, err := xproto.GetModifierMapping(conn.c).Reply()
	if err != nil || mm == nil {
		return heldState{}, false
	}
	codes := make([]uint8, len(mm.Keycodes))
	for i, k := range mm.Keycodes {
		codes[i] = uint8(k)
	}
	m := modifierMapFromReply(codes)
	keys, ok := waitForRelease(&m, limit, func() (keymap, bool) {
		r, err := xproto.QueryKeymap(conn.c).Reply()
		if err != nil || r == nil || len(r.Keys) < 32 {
			return keymap{}, false
		}
		return keymap(r.Keys[:32]), true
	})
	if !ok {
		return heldState{}, false
	}
	return heldStateFromKeymap(&m, &keys), true
}

// xtestKeys implements [platform.ChordKeys] over XTEST FakeInput.
type xtestKeys struct {
	conn *xConn
	km   kbMap
	key  uint8
	held heldState
}

func (k *xtestKeys) fake(press bool, keycode uint8) error {
	typ := byte(evKeyRelease)
	if press {
		typ = evKeyPress
	}
	if err := xtest.FakeInputChecked(k.conn.c, typ, keycode, 0, k.conn.root, 0, 0, 0).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

func (k *xtestKeys) modifier(m platform.KeyModifier, press bool) error {
	kc, ok := k.km.keycode(modifierKeysym(m))
	if !ok {
		return platform.Backend("x11: no keycode for modifier %s", m)
	}
	return k.fake(press, kc)
}

// ReleaseHeld releases the exact physical keycodes (e.g. Shift_R), not just the default
// keysym's.
func (k *xtestKeys) ReleaseHeld(m platform.KeyModifier) error {
	codes := k.held.keycodesOf(m)
	if len(codes) == 0 {
		return k.Release(m)
	}
	for _, c := range codes {
		if err := k.fake(false, c); err != nil {
			return err
		}
	}
	return nil
}

func (k *xtestKeys) Press(m platform.KeyModifier) error   { return k.modifier(m, true) }
func (k *xtestKeys) Release(m platform.KeyModifier) error { return k.modifier(m, false) }

func (k *xtestKeys) Click() error {
	if err := k.fake(true, k.key); err != nil {
		return err
	}
	return k.fake(false, k.key)
}
