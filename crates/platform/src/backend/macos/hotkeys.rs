//! Global hotkeys via `global-hotkey` (Carbon `RegisterEventHotKey`; no TCC permission).
use std::collections::HashMap;
use std::str::FromStr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, sync_channel};
use std::sync::{Arc, Mutex, PoisonError};

use global_hotkey::hotkey::{Code, HotKey, Modifiers};
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};

use crate::{Chord, HotkeyAction, HotkeyEvent, HotkeyHost, KeyModifier, PlatformError};

const CHANNEL_CAPACITY: usize = 32;

/// Translate a [`Chord`] into a `global-hotkey` [`HotKey`].
fn to_hotkey(chord: &Chord) -> Result<HotKey, PlatformError> {
    let code = Code::from_str(&chord.key).map_err(|_| PlatformError::InvalidChord(chord.to_string()))?;
    let mods = chord.modifiers.iter().fold(Modifiers::empty(), |acc, m| {
        acc | match m {
            KeyModifier::Ctrl => Modifiers::CONTROL,
            KeyModifier::Alt => Modifiers::ALT,
            KeyModifier::Shift => Modifiers::SHIFT,
            KeyModifier::Meta => Modifiers::META,
        }
    });
    Ok(HotKey::new(Some(mods), code))
}

fn map_error(chord: &Chord, e: global_hotkey::Error) -> PlatformError {
    match e {
        global_hotkey::Error::AlreadyRegistered(_) | global_hotkey::Error::FailedToRegister(_) => {
            PlatformError::HotkeyConflict(format!("{chord}: {e}"))
        }
        other => PlatformError::Backend(other.to_string()),
    }
}

pub(super) struct MacHotkeyHost {
    manager: GlobalHotKeyManager,
    /// action -> registered hotkey.
    registered: Mutex<HashMap<HotkeyAction, HotKey>>,
    /// hotkey id -> action, shared with the event handler.
    ids: Arc<Mutex<HashMap<u32, HotkeyAction>>>,
    events_taken: AtomicBool,
}

impl std::fmt::Debug for MacHotkeyHost {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("MacHotkeyHost").finish_non_exhaustive()
    }
}

impl MacHotkeyHost {
    pub(super) fn new() -> Result<Self, PlatformError> {
        let manager = GlobalHotKeyManager::new().map_err(|e| PlatformError::Backend(e.to_string()))?;
        Ok(Self {
            manager,
            registered: Mutex::new(HashMap::new()),
            ids: Arc::new(Mutex::new(HashMap::new())),
            events_taken: AtomicBool::new(false),
        })
    }
}

impl HotkeyHost for MacHotkeyHost {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        let hotkey = to_hotkey(chord)?;
        let mut registered = self.registered.lock().unwrap_or_else(PoisonError::into_inner);
        if let Some(old) = registered.remove(&id) {
            self.ids.lock().unwrap_or_else(PoisonError::into_inner).remove(&old.id());
            if let Err(e) = self.manager.unregister(old) {
                tracing::warn!(error = %e, ?id, "failed to unregister previous hotkey");
            }
        }
        self.manager.register(hotkey).map_err(|e| map_error(chord, e))?;
        self.ids.lock().unwrap_or_else(PoisonError::into_inner).insert(hotkey.id(), id);
        registered.insert(id, hotkey);
        Ok(())
    }

    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        let Some(hotkey) = self.registered.lock().unwrap_or_else(PoisonError::into_inner).remove(&id) else {
            return Ok(());
        };
        self.ids.lock().unwrap_or_else(PoisonError::into_inner).remove(&hotkey.id());
        self.manager.unregister(hotkey).map_err(|e| PlatformError::Backend(e.to_string()))
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        let hotkeys: Vec<HotKey> =
            self.registered.lock().unwrap_or_else(PoisonError::into_inner).drain().map(|(_, h)| h).collect();
        self.ids.lock().unwrap_or_else(PoisonError::into_inner).clear();
        self.manager.unregister_all(&hotkeys).map_err(|e| PlatformError::Backend(e.to_string()))
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        if self.events_taken.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        let (tx, rx) = sync_channel(CHANNEL_CAPACITY);
        let ids = Arc::clone(&self.ids);
        // Runs inside the Carbon event handler on the main thread: lookup + try_send only.
        GlobalHotKeyEvent::set_event_handler(Some(move |event: GlobalHotKeyEvent| {
            if event.state() != HotKeyState::Pressed {
                return;
            }
            let action = ids.lock().unwrap_or_else(PoisonError::into_inner).get(&event.id()).copied();
            if let Some(action) = action {
                let _ = tx.try_send(HotkeyEvent { action });
            }
        }));
        Ok(rx)
    }
}

/// Used when the Carbon event handler could not be installed.
#[derive(Debug)]
pub(super) struct DisabledHotkeys;

impl HotkeyHost for DisabledHotkeys {
    fn register(&self, _id: HotkeyAction, _chord: &Chord) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported("global hotkeys"))
    }

    fn unregister(&self, _id: HotkeyAction) -> Result<(), PlatformError> {
        Ok(())
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        Ok(())
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        Err(PlatformError::Unsupported("global hotkeys"))
    }
}
