package platformtest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

type chordScript struct {
	chord platform.Chord
	text  string
}

// InputSimulator records chords and, as the game would, writes scripted text into a
// [Clipboard] in response. With a delay (SetDelay) the write happens on a goroutine, which
// exercises the polling of [platform.CopyItemText].
type InputSimulator struct {
	clipboard *Clipboard

	mu sync.Mutex
	// scripts is the text per exact chord; other chords get response.
	scripts  []chordScript
	response string
	delay    time.Duration
	sent     []platform.Chord
	fail     bool
}

var _ platform.InputSimulator = (*InputSimulator)(nil)

// NewInputSimulator returns a simulator that writes into clipboard. Until it is scripted it
// only records chords.
func NewInputSimulator(clipboard *Clipboard) *InputSimulator {
	return &InputSimulator{clipboard: clipboard}
}

// Respond makes any chord without its own script copy text; "" copies nothing.
func (s *InputSimulator) Respond(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.response = text
}

// OnChord makes exactly chord copy text, whatever Respond says.
func (s *InputSimulator) OnChord(chord platform.Chord, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.scripts {
		if s.scripts[i].chord.Equal(chord) {
			s.scripts[i].text = text
			return
		}
	}
	s.scripts = append(s.scripts, chordScript{chord, text})
}

// SetDelay sets the delay between a chord and the clipboard write.
func (s *InputSimulator) SetDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = delay
}

// SetFailing makes SendChord fail with PermissionDenied(accessibility).
func (s *InputSimulator) SetFailing(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

// Sent returns the chords sent so far.
func (s *InputSimulator) Sent() []platform.Chord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sent)
}

// Clipboard is the clipboard this simulator writes to.
func (s *InputSimulator) Clipboard() *Clipboard { return s.clipboard }

// SendChord implements [platform.InputSimulator].
func (s *InputSimulator) SendChord(chord platform.Chord) error {
	s.mu.Lock()
	if s.fail {
		s.mu.Unlock()
		return platform.PermissionDenied(platform.PermissionAccessibility)
	}
	s.sent = append(s.sent, chord)
	text := s.response
	for _, sc := range s.scripts {
		if sc.chord.Equal(chord) {
			text = sc.text
			break
		}
	}
	delay := s.delay
	s.mu.Unlock()

	switch {
	case text == "":
	case delay == 0:
		s.clipboard.Put(text)
	default:
		go func() {
			time.Sleep(delay)
			s.clipboard.Put(text)
		}()
	}
	return nil
}

// InputListener is the passive input hook; the test injects events with Emit.
type InputListener struct {
	mu     sync.Mutex
	events stream[platform.InputEvent]
}

var _ platform.InputListener = (*InputListener)(nil)

// NewInputListener returns a listener that has not started.
func NewInputListener() *InputListener { return &InputListener{} }

// Emit injects an input event and reports whether it was delivered or buffered.
func (l *InputListener) Emit(event platform.InputEvent) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.emit(event)
}

// IsStarted reports whether Start was called.
func (l *InputListener) IsStarted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.started
}

// Start implements [platform.InputListener]. The channel is closed when ctx ends.
func (l *InputListener) Start(ctx context.Context) (<-chan platform.InputEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.subscribe(ctx, &l.mu)
}
