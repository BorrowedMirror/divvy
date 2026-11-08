package platform

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const copyTestItem = "Item Class: Rings\nRarity: Rare\n"

// mockClip is a Clipboard whose methods are set per test; a nil method fails the test when
// called.
type mockClip struct {
	t          *testing.T
	text       func() (string, bool, error)
	setText    func(string) error
	clear      func() error
	hasNonText func() bool
}

func (m *mockClip) Text() (string, bool, error) {
	if m.text == nil {
		m.t.Fatal("unexpected Text")
	}
	return m.text()
}

func (m *mockClip) SetText(s string) error {
	if m.setText == nil {
		m.t.Fatal("unexpected SetText")
	}
	return m.setText(s)
}

func (m *mockClip) Clear() error {
	if m.clear == nil {
		m.t.Fatal("unexpected Clear")
	}
	return m.clear()
}

func (m *mockClip) HasNonText() bool {
	if m.hasNonText == nil {
		m.t.Fatal("unexpected HasNonText")
	}
	return m.hasNonText()
}

type mockSim struct {
	t     *testing.T
	send  func(Chord) error
	calls int
}

func (m *mockSim) SendChord(c Chord) error {
	m.calls++
	if m.send == nil {
		m.t.Fatal("unexpected SendChord")
	}
	return m.send(c)
}

// clipState is shared clipboard contents so the simulator can "write" to it.
type clipState struct {
	mu   sync.Mutex
	text *string
}

func newClipState(text *string) *clipState { return &clipState{text: text} }

func (s *clipState) get() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text
}

func (s *clipState) set(text *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text = text
}

func textResult(p *string) (string, bool, error) {
	if p == nil {
		return "", false, nil
	}
	return *p, true, nil
}

// statefulClip is a clipboard mock backed by shared state.
func statefulClip(t *testing.T, s *clipState) *mockClip {
	t.Helper()
	return &mockClip{
		t:          t,
		text:       func() (string, bool, error) { return textResult(s.get()) },
		hasNonText: func() bool { return false },
		setText:    func(v string) error { s.set(&v); return nil },
		clear:      func() error { s.set(nil); return nil },
	}
}

func copyOpts() CopyOptions {
	o := DefaultCopyOptions()
	o.Timeout = 100 * time.Millisecond
	o.PollInterval = 10 * time.Millisecond
	return o
}

func TestLooksLikeItemChecksHeader(t *testing.T) {
	assert.True(t, LooksLikeItem("\r\n  Item Class: Rings"))
	assert.True(t, LooksLikeItem("Gegenstandsklasse: Ringe\nSeltenheit: Selten\nX\nSaphirring\n--------\nY"))
	assert.True(t, LooksLikeItem("物品種類: 戒指\n稀有度： 稀有\nX\n--------\nY"))
	assert.False(t, LooksLikeItem("Note: hi\nanother: line"))
	assert.False(t, LooksLikeItem("hello"))
	assert.False(t, LooksLikeItem(""))
}

func TestCopyChords(t *testing.T) {
	assert.Equal(t, "Ctrl+Alt+C", CopyChord(true).String())
	assert.Equal(t, "Ctrl+C", CopyChord(false).String())
}

func TestAdvancedCopyReturnsItemAndRestoresPreviousText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("previous"))
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(c Chord) error {
			assert.True(t, c.Equal(CopyChord(true)))
			assert.Nil(t, state.get(), "clipboard must be cleared before sending")
			state.set(new(copyTestItem))
			return nil
		}}
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, 1, sim.calls)
		assert.Equal(t, new("previous"), state.get())
	})
}

func TestPlainCopyWithoutRestoreKeepsItemOnClipboard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("previous"))
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(c Chord) error {
			assert.True(t, c.Equal(CopyChord(false)))
			state.set(new(copyTestItem))
			return nil
		}}
		o := copyOpts()
		o.Advanced = false
		o.RestoreClipboard = false
		text, err := CopyItemText(sim, clip, o)
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, new(copyTestItem), state.get())
	})
}

func TestItemArrivingLateIsPickedUpByPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(nil)
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(Chord) error {
			time.AfterFunc(25*time.Millisecond, func() { state.set(new(copyTestItem)) })
			return nil
		}}
		start := time.Now()
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, 30*time.Millisecond, time.Since(start), "seen on the fourth poll")
		// Previous clipboard was empty, so restoring clears it again.
		assert.Nil(t, state.get())
	})
}

func TestTimesOutWhenNothingArrives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("keep me"))
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		start := time.Now()
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrTimeout)
		pe, ok := errors.AsType[*Error](err)
		require.True(t, ok)
		assert.Equal(t, 100*time.Millisecond, pe.Timeout)
		assert.Equal(t, 100*time.Millisecond, time.Since(start))
		assert.Equal(t, new("keep me"), state.get())
	})
}

func TestNonItemTextReportsNoItemText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(nil)
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(Chord) error { state.set(new("not an item")); return nil }}
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrNoItemText)
	})
}

func TestZeroPollIntervalStillTerminates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(nil)
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		o := copyOpts()
		o.PollInterval = 0
		o.Timeout = 3 * time.Millisecond
		_, err := CopyItemText(sim, clip, o)
		require.ErrorIs(t, err, ErrTimeout)
	})
}

func TestSimulatorErrorIsReturnedAfterRestoring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("prev"))
		clip := statefulClip(t, state)
		sim := &mockSim{t: t, send: func(Chord) error { return Unsupported("keystroke_injection") }}
		start := time.Now()
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrUnsupported)
		assert.Zero(t, time.Since(start), "must not poll")
		assert.Equal(t, new("prev"), state.get())
	})
}

// seqText returns the given results in order, failing the test when called more often.
func seqText(t *testing.T, results ...func() (string, bool, error)) func() (string, bool, error) {
	t.Helper()
	i := 0
	return func() (string, bool, error) {
		if i >= len(results) {
			t.Fatalf("Text called %d times, expected %d", i+1, len(results))
		}
		r := results[i]
		i++
		return r()
	}
}

func seqErr(t *testing.T, name string, results ...error) func() error {
	t.Helper()
	i := 0
	return func() error {
		if i >= len(results) {
			t.Fatalf("%s called %d times, expected %d", name, i+1, len(results))
		}
		r := results[i]
		i++
		return r
	}
}

func textErr(msg string) func() (string, bool, error) {
	return func() (string, bool, error) { return "", false, Backend("%s", msg) }
}

func textOK(s string) func() (string, bool, error) {
	return func() (string, bool, error) { return s, true, nil }
}

func TestUnreadableClipboardAndFailingRestoreAreTolerated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Sequence: Text (err), Clear (ok), Text (err), Text x2 (item), Clear (err).
		var ops []string
		texts := seqText(t, textErr("odd"), textErr("busy"), textOK(copyTestItem), textOK(copyTestItem))
		clears := seqErr(t, "Clear", nil, Backend("gone"))
		clip := &mockClip{
			t:          t,
			hasNonText: func() bool { return false },
			text:       func() (string, bool, error) { ops = append(ops, "get"); return texts() },
			clear:      func() error { ops = append(ops, "clear"); return clears() },
		}
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, []string{"get", "clear", "get", "get", "get", "clear"}, ops)
	})
}

func TestClearFailureAbortsBeforeSending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clip := &mockClip{
			t:          t,
			text:       func() (string, bool, error) { return "", false, nil },
			hasNonText: func() bool { return false },
			clear:      func() error { return Backend("no clipboard") },
		}
		sim := &mockSim{t: t}
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrBackend)
		assert.Equal(t, 0, sim.calls)
	})
}

func TestImageOnClipboardIsNeitherClearedNorRestored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		item := newClipState(nil)
		clip := &mockClip{
			t:          t,
			text:       func() (string, bool, error) { return textResult(item.get()) },
			hasNonText: func() bool { return item.get() == nil },
			// clear and setText nil: never called.
		}
		sim := &mockSim{t: t, send: func(Chord) error { item.set(new(copyTestItem)); return nil }}
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
	})
}

// clipWhereUserCopiesAfter is a clipboard where the user copies user (nil = nothing) on the
// nthGet-th Text call. Text calls: 1 = previous, 2 = poll (sees the item), 3 = restore check.
func clipWhereUserCopiesAfter(t *testing.T, s *clipState, nthGet int, user *string) *mockClip {
	t.Helper()
	calls := 0
	clears := 0
	t.Cleanup(func() { assert.Equal(t, 1, clears, "Clear calls") })
	return &mockClip{
		t: t,
		text: func() (string, bool, error) {
			calls++
			if calls == nthGet {
				s.set(user)
			}
			return textResult(s.get())
		},
		hasNonText: func() bool { return false },
		clear: func() error {
			clears++
			s.set(nil)
			return nil
		},
	}
}

func TestUserCopyAfterTheItemIsNotOverwritten(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("previous"))
		clip := clipWhereUserCopiesAfter(t, state, 3, new("user copy"))
		sim := &mockSim{t: t, send: func(Chord) error { state.set(new(copyTestItem)); return nil }}
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, new("user copy"), state.get())
	})
}

func TestUserCopyDuringATimeoutIsKept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(new("previous"))
		// The user copies non-item text while we poll: NoItemText, and their copy stays.
		clip := clipWhereUserCopiesAfter(t, state, 4, new("user copy"))
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrNoItemText)
		assert.Equal(t, new("user copy"), state.get())
	})
}

func TestImageCopiedByUserDuringCopyIsKept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := newClipState(nil)
		calls := 0
		clears := 0
		clip := &mockClip{
			t:    t,
			text: func() (string, bool, error) { return textResult(state.get()) },
			// First check (before clearing): no image. Restore check: the user copied an image.
			hasNonText: func() bool { calls++; return calls > 1 },
			clear:      func() error { clears++; return nil },
		}
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		_, err := CopyItemText(sim, clip, copyOpts())
		require.ErrorIs(t, err, ErrTimeout)
		assert.Equal(t, 1, clears)
	})
}

func TestUnreadableClipboardAtRestoreTimeIsLeftAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ops []string
		texts := seqText(t, textOK("prev"), textOK(copyTestItem), textErr("busy"))
		clears := seqErr(t, "Clear", nil)
		clip := &mockClip{
			t:          t,
			hasNonText: func() bool { return false },
			text:       func() (string, bool, error) { ops = append(ops, "get"); return texts() },
			clear:      func() error { ops = append(ops, "clear"); return clears() },
		}
		sim := &mockSim{t: t, send: func(Chord) error { return nil }}
		text, err := CopyItemText(sim, clip, copyOpts())
		require.NoError(t, err)
		assert.Equal(t, copyTestItem, text)
		assert.Equal(t, []string{"get", "clear", "get", "get"}, ops)
	})
}
