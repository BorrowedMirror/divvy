//! Deterministic in-memory fakes for every `rkl_platform` trait.
//!
//! Every fake is a cheap cloneable *handle* over shared state (`Arc<Mutex<..>>`): box one clone
//! into a [`Platform`] and keep another in the test to drive it (`fire`, `emit`, `set_window`)
//! or inspect it (`calls`, `sent`, `history`).
//!
//! ```
//! use rkl_testkit::fakes::FakePlatformBuilder;
//! use rkl_platform::HotkeyAction;
//!
//! let (platform, fakes) = FakePlatformBuilder::new().item_text("Item Class: Stackable Currency\n").build();
//! let rx = platform.hotkeys.events().unwrap();
//! fakes.hotkeys.fire(HotkeyAction::PriceCheck);
//! assert_eq!(rx.try_recv().unwrap().action, HotkeyAction::PriceCheck);
//! ```
use std::collections::HashMap;
use std::sync::mpsc::{Receiver, Sender, channel};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use rkl_platform::{
    BackendKind, Capabilities, Chord, Clipboard, GAME_WINDOW_TITLE, HotkeyAction, HotkeyEvent, HotkeyHost, InputEvent,
    InputListener, InputSimulator, OverlayWindow, Platform, PlatformError, Rect, WindowEvent, WindowInfo,
    WindowTracker,
};

/// Lock ignoring poisoning: a panicking test thread must not cascade into unrelated asserts.
fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(PoisonError::into_inner)
}

/// A one-shot event stream: `subscribe` may be called once, later calls get `AlreadyStarted`.
/// Events emitted before subscription are buffered and delivered on subscribe.
#[derive(Debug)]
struct Stream<T> {
    sender: Option<Sender<T>>,
    started: bool,
    pending: Vec<T>,
}

impl<T> Default for Stream<T> {
    fn default() -> Self {
        Self { sender: None, started: false, pending: Vec::new() }
    }
}

impl<T> Stream<T> {
    fn subscribe(&mut self) -> Result<Receiver<T>, PlatformError> {
        if self.started {
            return Err(PlatformError::AlreadyStarted);
        }
        self.started = true;
        let (tx, rx) = channel();
        for ev in self.pending.drain(..) {
            let _ = tx.send(ev);
        }
        self.sender = Some(tx);
        Ok(rx)
    }

    /// Returns `true` when the event reached a live receiver (or was buffered pre-subscribe).
    fn emit(&mut self, ev: T) -> bool {
        match &self.sender {
            Some(tx) => tx.send(ev).is_ok(),
            None if !self.started => {
                self.pending.push(ev);
                true
            }
            None => false,
        }
    }
}

// ---------------------------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------------------------

/// One recorded clipboard operation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ClipboardOp {
    Get,
    Set(String),
    Clear,
}

#[derive(Debug, Default)]
struct ClipboardState {
    text: Option<String>,
    history: Vec<ClipboardOp>,
    fail: bool,
}

/// In-memory clipboard shared between clones.
#[derive(Debug, Clone, Default)]
pub struct FakeClipboard {
    state: Arc<Mutex<ClipboardState>>,
}

impl FakeClipboard {
    pub fn new() -> Self {
        Self::default()
    }

    /// Clipboard pre-filled with `text`.
    pub fn with_text(text: &str) -> Self {
        let c = Self::new();
        lock(&c.state).text = Some(text.to_owned());
        c
    }

    /// Current contents without recording a `Get`.
    pub fn peek(&self) -> Option<String> {
        lock(&self.state).text.clone()
    }

    /// Overwrite contents without recording a `Set` (as if another app copied).
    pub fn put(&self, text: Option<&str>) {
        lock(&self.state).text = text.map(str::to_owned);
    }

    /// Every operation performed through the [`Clipboard`] trait, in order.
    pub fn history(&self) -> Vec<ClipboardOp> {
        lock(&self.state).history.clone()
    }

    /// Make every trait call fail with `PlatformError::Backend` until reset.
    pub fn set_failing(&self, fail: bool) {
        lock(&self.state).fail = fail;
    }

    fn check(st: &ClipboardState) -> Result<(), PlatformError> {
        if st.fail { Err(PlatformError::Backend("fake clipboard failure".into())) } else { Ok(()) }
    }
}

impl Clipboard for FakeClipboard {
    fn get_text(&self) -> Result<Option<String>, PlatformError> {
        let mut st = lock(&self.state);
        Self::check(&st)?;
        st.history.push(ClipboardOp::Get);
        Ok(st.text.clone())
    }

    fn set_text(&self, text: &str) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        Self::check(&st)?;
        st.history.push(ClipboardOp::Set(text.to_owned()));
        st.text = Some(text.to_owned());
        Ok(())
    }

    fn clear(&self) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        Self::check(&st)?;
        st.history.push(ClipboardOp::Clear);
        st.text = None;
        Ok(())
    }
}

// ---------------------------------------------------------------------------------------------
// Input simulator
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Default)]
struct SimulatorState {
    /// Text per exact chord; falls back to `default_text`.
    scripts: HashMap<Chord, String>,
    default_text: Option<String>,
    delay: Duration,
    sent: Vec<Chord>,
    fail: bool,
}

/// Records chords and, like the game would, writes scripted text into a [`FakeClipboard`]
/// in response. With a non-zero [`delay`](Self::with_delay) the write happens on a background
/// thread, which exercises the polling loop of `copy_item_text`.
#[derive(Debug, Clone)]
pub struct FakeInputSimulator {
    clipboard: FakeClipboard,
    state: Arc<Mutex<SimulatorState>>,
}

impl FakeInputSimulator {
    /// Simulator that writes into `clipboard`. With no script it only records chords.
    pub fn new(clipboard: FakeClipboard) -> Self {
        Self { clipboard, state: Arc::default() }
    }

    /// Respond to *any* chord with `text`.
    #[must_use]
    pub fn responding(self, text: &str) -> Self {
        lock(&self.state).default_text = Some(text.to_owned());
        self
    }

    /// Respond to exactly `chord` with `text` (takes precedence over [`responding`](Self::responding)).
    #[must_use]
    pub fn on_chord(self, chord: Chord, text: &str) -> Self {
        lock(&self.state).scripts.insert(chord, text.to_owned());
        self
    }

    /// Delay before the clipboard is written.
    #[must_use]
    pub fn with_delay(self, delay: Duration) -> Self {
        lock(&self.state).delay = delay;
        self
    }

    /// Change the default response at runtime (`None` = game copies nothing).
    pub fn set_response(&self, text: Option<&str>) {
        lock(&self.state).default_text = text.map(str::to_owned);
    }

    /// Make `send_chord` fail with `PlatformError::PermissionDenied("accessibility")`.
    pub fn set_failing(&self, fail: bool) {
        lock(&self.state).fail = fail;
    }

    /// Chords sent so far.
    pub fn sent(&self) -> Vec<Chord> {
        lock(&self.state).sent.clone()
    }

    /// The clipboard this simulator writes to.
    pub fn clipboard(&self) -> &FakeClipboard {
        &self.clipboard
    }
}

impl InputSimulator for FakeInputSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        let (text, delay) = {
            let mut st = lock(&self.state);
            if st.fail {
                return Err(PlatformError::PermissionDenied("accessibility"));
            }
            st.sent.push(chord.clone());
            (st.scripts.get(chord).cloned().or_else(|| st.default_text.clone()), st.delay)
        };
        let Some(text) = text else { return Ok(()) };
        if delay.is_zero() {
            self.clipboard.put(Some(&text));
        } else {
            let clip = self.clipboard.clone();
            std::thread::spawn(move || {
                std::thread::sleep(delay);
                clip.put(Some(&text));
            });
        }
        Ok(())
    }
}

// ---------------------------------------------------------------------------------------------
// Window tracker
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Default)]
struct TrackerState {
    window: Option<WindowInfo>,
    events: Stream<WindowEvent>,
}

/// Window tracker whose game window is set by the test.
#[derive(Debug, Clone, Default)]
pub struct FakeWindowTracker {
    state: Arc<Mutex<TrackerState>>,
}

/// A plausible focused game window at `rect`.
pub fn game_window(rect: Rect) -> WindowInfo {
    WindowInfo { title: GAME_WINDOW_TITLE.to_owned(), rect, focused: true, native_id: 0x00c0_ffee }
}

impl FakeWindowTracker {
    pub fn new() -> Self {
        Self::default()
    }

    /// Tracker that already sees `window`.
    pub fn with_window(window: WindowInfo) -> Self {
        let t = Self::new();
        lock(&t.state).window = Some(window);
        t
    }

    /// Replace the window and emit the matching event (`Found`, `Moved`, `FocusChanged`, `Lost`).
    pub fn set_window(&self, window: Option<WindowInfo>) {
        let mut st = lock(&self.state);
        let prev = st.window.take();
        let events: Vec<WindowEvent> = match (&prev, &window) {
            (None, None) => vec![],
            (Some(_), None) => vec![WindowEvent::Lost],
            (None, Some(w)) => vec![WindowEvent::Found(w.clone())],
            (Some(p), Some(w)) if p.native_id != w.native_id || p.title != w.title => {
                vec![WindowEvent::Found(w.clone())]
            }
            (Some(p), Some(w)) => {
                let mut v = Vec::new();
                if p.rect != w.rect {
                    v.push(WindowEvent::Moved { rect: w.rect });
                }
                if p.focused != w.focused {
                    v.push(WindowEvent::FocusChanged { focused: w.focused });
                }
                v
            }
        };
        st.window = window;
        for ev in events {
            st.events.emit(ev);
        }
    }

    /// Change only the focus flag (emits `FocusChanged` when it changes).
    pub fn set_focused(&self, focused: bool) {
        let w = lock(&self.state).window.clone();
        if let Some(mut w) = w {
            w.focused = focused;
            self.set_window(Some(w));
        }
    }

    /// Emit a raw event without touching state. Returns whether a receiver got it.
    pub fn emit(&self, event: WindowEvent) -> bool {
        lock(&self.state).events.emit(event)
    }
}

impl WindowTracker for FakeWindowTracker {
    fn game_window(&self) -> Option<WindowInfo> {
        lock(&self.state).window.clone()
    }

    fn is_game_focused(&self) -> bool {
        lock(&self.state).window.as_ref().is_some_and(|w| w.focused)
    }

    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError> {
        lock(&self.state).events.subscribe()
    }
}

// ---------------------------------------------------------------------------------------------
// Hotkeys
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Default)]
struct HotkeyState {
    registered: HashMap<HotkeyAction, Chord>,
    /// Chords "owned by another application": registering them fails.
    taken: Vec<Chord>,
    events: Stream<HotkeyEvent>,
}

/// Hotkey host; the test presses a hotkey with [`fire`](Self::fire).
#[derive(Debug, Clone, Default)]
pub struct FakeHotkeyHost {
    state: Arc<Mutex<HotkeyState>>,
}

impl FakeHotkeyHost {
    pub fn new() -> Self {
        Self::default()
    }

    /// Simulate the user pressing the hotkey for `action`. Fires even if `action` is not
    /// registered (tests of the consumer usually don't care); returns whether it was delivered.
    pub fn fire(&self, action: HotkeyAction) -> bool {
        lock(&self.state).events.emit(HotkeyEvent { action })
    }

    /// Simulate pressing `chord`: fires the action bound to it, if any.
    pub fn press(&self, chord: &Chord) -> Option<HotkeyAction> {
        let mut st = lock(&self.state);
        let action = st.registered.iter().find(|(_, c)| *c == chord).map(|(a, _)| *a)?;
        st.events.emit(HotkeyEvent { action });
        Some(action)
    }

    /// Mark `chord` as grabbed by another application.
    pub fn block(&self, chord: Chord) {
        lock(&self.state).taken.push(chord);
    }

    /// Current registrations.
    pub fn registered(&self) -> HashMap<HotkeyAction, Chord> {
        lock(&self.state).registered.clone()
    }
}

impl HotkeyHost for FakeHotkeyHost {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        if st.taken.contains(chord) || st.registered.iter().any(|(a, c)| *a != id && c == chord) {
            return Err(PlatformError::HotkeyConflict(chord.to_string()));
        }
        st.registered.insert(id, chord.clone());
        Ok(())
    }

    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        lock(&self.state).registered.remove(&id);
        Ok(())
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        lock(&self.state).registered.clear();
        Ok(())
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        lock(&self.state).events.subscribe()
    }
}

// ---------------------------------------------------------------------------------------------
// Input listener
// ---------------------------------------------------------------------------------------------

/// Passive hook; the test injects events with [`emit`](Self::emit).
#[derive(Debug, Clone, Default)]
pub struct FakeInputListener {
    state: Arc<Mutex<Stream<InputEvent>>>,
}

impl FakeInputListener {
    pub fn new() -> Self {
        Self::default()
    }

    /// Inject an input event. Returns whether it was delivered/buffered.
    pub fn emit(&self, event: InputEvent) -> bool {
        lock(&self.state).emit(event)
    }

    pub fn is_started(&self) -> bool {
        lock(&self.state).started
    }
}

impl InputListener for FakeInputListener {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        lock(&self.state).subscribe()
    }
}

// ---------------------------------------------------------------------------------------------
// Overlay
// ---------------------------------------------------------------------------------------------

/// One recorded [`OverlayWindow`] call.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OverlayCall {
    Prepare,
    ShowAt(Rect),
    Hide,
    SetClickThrough(bool),
}

#[derive(Debug, Default)]
struct OverlayState {
    calls: Vec<OverlayCall>,
    visible: bool,
    rect: Option<Rect>,
    click_through: bool,
}

/// Overlay styler that records every call.
#[derive(Debug, Clone, Default)]
pub struct FakeOverlay {
    state: Arc<Mutex<OverlayState>>,
}

impl FakeOverlay {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn calls(&self) -> Vec<OverlayCall> {
        lock(&self.state).calls.clone()
    }

    pub fn is_visible(&self) -> bool {
        lock(&self.state).visible
    }

    /// Rect of the last `show_at`.
    pub fn rect(&self) -> Option<Rect> {
        lock(&self.state).rect
    }

    pub fn is_click_through(&self) -> bool {
        lock(&self.state).click_through
    }
}

impl OverlayWindow for FakeOverlay {
    fn prepare(&self) -> Result<(), PlatformError> {
        lock(&self.state).calls.push(OverlayCall::Prepare);
        Ok(())
    }

    fn show_at(&self, rect: Rect) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        st.calls.push(OverlayCall::ShowAt(rect));
        st.visible = true;
        st.rect = Some(rect);
        Ok(())
    }

    fn hide(&self) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        st.calls.push(OverlayCall::Hide);
        st.visible = false;
        Ok(())
    }

    fn set_click_through(&self, enabled: bool) -> Result<(), PlatformError> {
        let mut st = lock(&self.state);
        st.calls.push(OverlayCall::SetClickThrough(enabled));
        st.click_through = enabled;
        Ok(())
    }
}

// ---------------------------------------------------------------------------------------------
// Platform
// ---------------------------------------------------------------------------------------------

/// Capabilities reported by the fake backend: everything works.
pub fn fake_capabilities() -> Capabilities {
    Capabilities {
        window_tracking: true,
        overlay_always_on_top: true,
        overlay_click_through: true,
        global_hotkeys: true,
        passive_hook: true,
        keystroke_injection: true,
        clipboard: true,
        missing_permissions: vec![],
        notes: vec!["fake backend (rkl-testkit)".to_owned()],
    }
}

/// Test-side handles to the fakes inside a [`Platform`] built by [`FakePlatformBuilder`].
#[derive(Debug, Clone)]
pub struct FakeHandles {
    pub clipboard: FakeClipboard,
    pub input_simulator: FakeInputSimulator,
    pub window_tracker: FakeWindowTracker,
    pub hotkeys: FakeHotkeyHost,
    pub input_listener: FakeInputListener,
}

/// Builds a [`Platform`] of fakes plus the [`FakeHandles`] to drive them.
#[derive(Debug, Default)]
pub struct FakePlatformBuilder {
    item_text: Option<String>,
    advanced_text: Option<String>,
    delay: Duration,
    window: Option<WindowInfo>,
    clipboard: Option<String>,
    capabilities: Option<Capabilities>,
}

impl FakePlatformBuilder {
    pub fn new() -> Self {
        Self::default()
    }

    /// Text the "game" copies on any chord.
    #[must_use]
    pub fn item_text(mut self, text: &str) -> Self {
        self.item_text = Some(text.to_owned());
        self
    }

    /// Text the "game" copies on `Ctrl+Alt+C` (advanced copy).
    #[must_use]
    pub fn advanced_text(mut self, text: &str) -> Self {
        self.advanced_text = Some(text.to_owned());
        self
    }

    /// Delay between the chord and the clipboard write.
    #[must_use]
    pub fn copy_delay(mut self, delay: Duration) -> Self {
        self.delay = delay;
        self
    }

    /// Game window the tracker reports.
    #[must_use]
    pub fn window(mut self, window: WindowInfo) -> Self {
        self.window = Some(window);
        self
    }

    /// Initial clipboard contents (e.g. to test restore).
    #[must_use]
    pub fn clipboard(mut self, text: &str) -> Self {
        self.clipboard = Some(text.to_owned());
        self
    }

    #[must_use]
    pub fn capabilities(mut self, caps: Capabilities) -> Self {
        self.capabilities = Some(caps);
        self
    }

    pub fn build(self) -> (Platform, FakeHandles) {
        let clipboard = self.clipboard.as_deref().map_or_else(FakeClipboard::new, FakeClipboard::with_text);
        let mut sim = FakeInputSimulator::new(clipboard.clone()).with_delay(self.delay);
        if let Some(t) = &self.item_text {
            sim = sim.responding(t);
        }
        if let Some(t) = &self.advanced_text {
            sim = sim.on_chord(advanced_copy_chord(), t);
        }
        let window_tracker = self.window.map_or_else(FakeWindowTracker::new, FakeWindowTracker::with_window);
        let handles = FakeHandles {
            clipboard,
            input_simulator: sim,
            window_tracker,
            hotkeys: FakeHotkeyHost::new(),
            input_listener: FakeInputListener::new(),
        };
        let platform = Platform {
            kind: BackendKind::Fake,
            capabilities: self.capabilities.unwrap_or_else(fake_capabilities),
            window_tracker: Box::new(handles.window_tracker.clone()),
            hotkeys: Box::new(handles.hotkeys.clone()),
            input_listener: Box::new(handles.input_listener.clone()),
            input_simulator: Box::new(handles.input_simulator.clone()),
            clipboard: Box::new(handles.clipboard.clone()),
        };
        (platform, handles)
    }
}

/// `Ctrl+C`.
pub fn copy_chord() -> Chord {
    Chord::new(&[rkl_platform::KeyModifier::Ctrl], "KeyC")
}

/// `Ctrl+Alt+C`.
pub fn advanced_copy_chord() -> Chord {
    Chord::new(&[rkl_platform::KeyModifier::Ctrl, rkl_platform::KeyModifier::Alt], "KeyC")
}

/// A fake [`Platform`] with defaults: empty clipboard, no game window, nothing scripted.
/// Use [`FakePlatformBuilder`] to keep handles for driving it.
pub fn fake_platform() -> Platform {
    FakePlatformBuilder::new().build().0
}

#[cfg(test)]
mod tests {
    use super::*;
    use rkl_platform::KeyModifier;
    use std::time::Instant;

    const ITEM: &str = "Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n";

    fn rect(x: i32) -> Rect {
        Rect { x, y: 0, width: 800, height: 600 }
    }

    #[test]
    fn clipboard_shares_state_and_records_history() {
        let a = FakeClipboard::new();
        let b = a.clone();
        assert_eq!(a.get_text().unwrap(), None);
        b.set_text("hi").unwrap();
        assert_eq!(a.get_text().unwrap().as_deref(), Some("hi"));
        a.clear().unwrap();
        assert_eq!(b.peek(), None);
        assert_eq!(
            a.history(),
            vec![ClipboardOp::Get, ClipboardOp::Set("hi".into()), ClipboardOp::Get, ClipboardOp::Clear]
        );
        b.put(Some("external"));
        assert_eq!(a.peek().as_deref(), Some("external"));
        assert_eq!(a.history().len(), 4, "put/peek are not recorded");
    }

    #[test]
    fn clipboard_failure_injection() {
        let c = FakeClipboard::with_text("x");
        c.set_failing(true);
        assert!(matches!(c.get_text(), Err(PlatformError::Backend(_))));
        assert!(c.set_text("y").is_err());
        assert!(c.clear().is_err());
        c.set_failing(false);
        assert_eq!(c.get_text().unwrap().as_deref(), Some("x"));
    }

    #[test]
    fn simulator_writes_scripted_text() {
        let clip = FakeClipboard::new();
        let sim = FakeInputSimulator::new(clip.clone()).responding(ITEM).on_chord(advanced_copy_chord(), "ADV");
        sim.send_chord(&copy_chord()).unwrap();
        assert_eq!(clip.peek().as_deref(), Some(ITEM));
        sim.send_chord(&advanced_copy_chord()).unwrap();
        assert_eq!(clip.peek().as_deref(), Some("ADV"));
        assert_eq!(sim.sent(), vec![copy_chord(), advanced_copy_chord()]);
        assert!(std::ptr::eq(sim.clipboard(), sim.clipboard()));
    }

    #[test]
    fn simulator_without_script_only_records() {
        let clip = FakeClipboard::with_text("old");
        let sim = FakeInputSimulator::new(clip.clone());
        sim.send_chord(&copy_chord()).unwrap();
        assert_eq!(clip.peek().as_deref(), Some("old"));
        sim.set_response(Some("new"));
        sim.send_chord(&copy_chord()).unwrap();
        assert_eq!(clip.peek().as_deref(), Some("new"));
        sim.set_response(None);
        clip.put(None);
        sim.send_chord(&copy_chord()).unwrap();
        assert_eq!(clip.peek(), None);
    }

    #[test]
    fn simulator_delay_writes_later() {
        let clip = FakeClipboard::new();
        let sim = FakeInputSimulator::new(clip.clone()).responding(ITEM).with_delay(Duration::from_millis(30));
        sim.send_chord(&copy_chord()).unwrap();
        assert_eq!(clip.peek(), None, "not written synchronously");
        let deadline = Instant::now() + Duration::from_secs(5);
        while clip.peek().is_none() {
            assert!(Instant::now() < deadline, "delayed write never happened");
            std::thread::sleep(Duration::from_millis(5));
        }
        assert_eq!(clip.peek().as_deref(), Some(ITEM));
    }

    #[test]
    fn simulator_failure() {
        let sim = FakeInputSimulator::new(FakeClipboard::new()).responding(ITEM);
        sim.set_failing(true);
        assert!(matches!(sim.send_chord(&copy_chord()), Err(PlatformError::PermissionDenied("accessibility"))));
        assert!(sim.sent().is_empty());
        assert_eq!(sim.clipboard().peek(), None);
    }

    #[test]
    fn tracker_emits_transitions() {
        let t = FakeWindowTracker::new();
        assert!(t.game_window().is_none());
        assert!(!t.is_game_focused());
        let rx = t.subscribe().unwrap();
        assert!(matches!(t.subscribe(), Err(PlatformError::AlreadyStarted)));

        t.set_window(None);
        t.set_window(Some(game_window(rect(0))));
        t.set_window(Some(game_window(rect(10))));
        t.set_focused(false);
        t.set_focused(false);
        let mut other = game_window(rect(10));
        other.native_id = 2;
        t.set_window(Some(other.clone()));
        t.set_window(None);
        t.set_focused(true); // no window: no-op

        let events: Vec<_> = rx.try_iter().collect();
        assert_eq!(
            events,
            vec![
                WindowEvent::Found(game_window(rect(0))),
                WindowEvent::Moved { rect: rect(10) },
                WindowEvent::FocusChanged { focused: false },
                WindowEvent::Found(other),
                WindowEvent::Lost,
            ]
        );
        assert!(t.emit(WindowEvent::Lost));
        drop(rx);
        assert!(!t.emit(WindowEvent::Lost));
    }

    #[test]
    fn tracker_with_window_and_presubscribe_buffering() {
        let t = FakeWindowTracker::with_window(game_window(rect(1)));
        assert!(t.is_game_focused());
        assert_eq!(t.game_window().unwrap().title, GAME_WINDOW_TITLE);
        assert!(t.emit(WindowEvent::FocusChanged { focused: true }));
        let rx = t.subscribe().unwrap();
        assert_eq!(rx.try_recv().unwrap(), WindowEvent::FocusChanged { focused: true });
    }

    #[test]
    fn hotkeys_register_fire_and_conflict() {
        let h = FakeHotkeyHost::new();
        let d = Chord::parse("Ctrl+D").unwrap();
        let alt_d = Chord::parse("Ctrl+Alt+D").unwrap();
        h.register(HotkeyAction::PriceCheck, &d).unwrap();
        // re-registering the same action with the same chord is fine
        h.register(HotkeyAction::PriceCheck, &d).unwrap();
        assert!(matches!(h.register(HotkeyAction::PriceCheckPinned, &d), Err(PlatformError::HotkeyConflict(_))));
        h.block(alt_d.clone());
        assert!(matches!(h.register(HotkeyAction::PriceCheckPinned, &alt_d), Err(PlatformError::HotkeyConflict(_))));
        assert_eq!(h.registered().len(), 1);

        let rx = h.events().unwrap();
        assert!(h.events().is_err());
        assert!(h.fire(HotkeyAction::OpenSettings));
        assert_eq!(h.press(&d), Some(HotkeyAction::PriceCheck));
        assert_eq!(h.press(&alt_d), None);
        let got: Vec<_> = rx.try_iter().map(|e| e.action).collect();
        assert_eq!(got, vec![HotkeyAction::OpenSettings, HotkeyAction::PriceCheck]);

        h.unregister(HotkeyAction::PriceCheck).unwrap();
        assert!(h.registered().is_empty());
        h.register(HotkeyAction::PriceCheck, &d).unwrap();
        h.register(HotkeyAction::OpenSettings, &Chord::new(&[KeyModifier::Ctrl], "Comma")).unwrap();
        h.unregister_all().unwrap();
        assert!(h.registered().is_empty());
    }

    #[test]
    fn input_listener_starts_once() {
        let l = FakeInputListener::new();
        assert!(!l.is_started());
        assert!(l.emit(InputEvent::Wheel { delta: 1 }));
        let rx = l.start().unwrap();
        assert!(l.is_started());
        assert!(matches!(l.start(), Err(PlatformError::AlreadyStarted)));
        assert!(l.emit(InputEvent::KeyDown { key: "Escape".into() }));
        assert_eq!(
            rx.try_iter().collect::<Vec<_>>(),
            vec![InputEvent::Wheel { delta: 1 }, InputEvent::KeyDown { key: "Escape".into() }]
        );
    }

    #[test]
    fn overlay_records_calls() {
        let o = FakeOverlay::new();
        let o2 = o.clone();
        o.prepare().unwrap();
        o.show_at(rect(5)).unwrap();
        assert!(o2.is_visible());
        assert_eq!(o2.rect(), Some(rect(5)));
        o.set_click_through(true).unwrap();
        assert!(o2.is_click_through());
        o.hide().unwrap();
        assert!(!o2.is_visible());
        assert_eq!(
            o2.calls(),
            vec![
                OverlayCall::Prepare,
                OverlayCall::ShowAt(rect(5)),
                OverlayCall::SetClickThrough(true),
                OverlayCall::Hide
            ]
        );
    }

    #[test]
    fn platform_builder_wires_everything() {
        let (p, h) = FakePlatformBuilder::new()
            .item_text(ITEM)
            .advanced_text("ADV")
            .window(game_window(rect(0)))
            .clipboard("previous")
            .build();
        assert_eq!(p.kind, BackendKind::Fake);
        assert!(p.capabilities.clipboard);
        assert_eq!(p.clipboard.get_text().unwrap().as_deref(), Some("previous"));
        p.input_simulator.send_chord(&copy_chord()).unwrap();
        assert_eq!(h.clipboard.peek().as_deref(), Some(ITEM));
        p.input_simulator.send_chord(&advanced_copy_chord()).unwrap();
        assert_eq!(p.clipboard.get_text().unwrap().as_deref(), Some("ADV"));
        assert!(p.window_tracker.is_game_focused());
        let rx = p.input_listener.start().unwrap();
        h.input_listener.emit(InputEvent::MouseMove { x: 1, y: 2 });
        assert_eq!(rx.try_recv().unwrap(), InputEvent::MouseMove { x: 1, y: 2 });
        assert_eq!(h.input_simulator.sent().len(), 2);
        assert!(format!("{p:?}").contains("Fake"));
    }

    #[test]
    fn platform_builder_custom_caps_and_delay() {
        let caps = Capabilities { clipboard: false, ..Capabilities::default() };
        let (p, h) = FakePlatformBuilder::new()
            .capabilities(caps.clone())
            .item_text(ITEM)
            .copy_delay(Duration::from_millis(1))
            .build();
        assert_eq!(p.capabilities, caps);
        p.input_simulator.send_chord(&copy_chord()).unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        while h.clipboard.peek().is_none() {
            assert!(Instant::now() < deadline);
            std::thread::sleep(Duration::from_millis(2));
        }
    }

    #[test]
    fn default_fake_platform() {
        let p = fake_platform();
        assert!(p.window_tracker.game_window().is_none());
        assert_eq!(p.clipboard.get_text().unwrap(), None);
        p.input_simulator.send_chord(&copy_chord()).unwrap();
        assert_eq!(p.clipboard.get_text().unwrap(), None);
        assert_eq!(fake_capabilities().notes.len(), 1);
    }
}
