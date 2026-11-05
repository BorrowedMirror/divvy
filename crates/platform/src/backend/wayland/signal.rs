//! Signal triggers: the hotkey fallback when no GlobalShortcuts portal exists.
//! SIGUSR2 -> `PriceCheck`, SIGUSR1 -> `PriceCheckPinned`. Users bind
//! `pkill -USR2 rkl` / `pkill -USR1 rkl` (or `rkl --trigger`) in their compositor.
use std::sync::mpsc::{Receiver, SyncSender, TrySendError, sync_channel};

use signal_hook::consts::{SIGUSR1, SIGUSR2};
use signal_hook::iterator::Signals;

use crate::{HotkeyAction, HotkeyEvent, PlatformError};

/// The action a trigger signal stands for.
pub(crate) fn action_for_signal(signal: i32) -> Option<HotkeyAction> {
    match signal {
        SIGUSR2 => Some(HotkeyAction::PriceCheck),
        SIGUSR1 => Some(HotkeyAction::PriceCheckPinned),
        _ => None,
    }
}

/// Start listening for SIGUSR2 (`PriceCheck`) and SIGUSR1 (`PriceCheckPinned`).
pub fn sigusr2_trigger() -> Result<Receiver<HotkeyEvent>, PlatformError> {
    let (tx, rx) = sync_channel(16);
    spawn_sigusr2(tx)?;
    Ok(rx)
}

/// Spawn the signal thread for SIGUSR2 and SIGUSR1.
pub(crate) fn spawn_sigusr2(tx: SyncSender<HotkeyEvent>) -> Result<(), PlatformError> {
    let mut signals =
        Signals::new([SIGUSR2, SIGUSR1]).map_err(|e| PlatformError::Backend(format!("SIGUSR1/2 handler: {e}")))?;
    std::thread::Builder::new()
        .name("rkl-signals".into())
        .spawn(move || {
            for signal in signals.forever() {
                let Some(action) = action_for_signal(signal) else { continue };
                match tx.try_send(HotkeyEvent { action }) {
                    Ok(()) | Err(TrySendError::Full(_)) => {}
                    Err(TrySendError::Disconnected(_)) => break,
                }
            }
        })
        .map_err(|e| PlatformError::Backend(format!("spawn signal thread: {e}")))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::*;

    #[test]
    fn signals_map_to_actions() {
        assert_eq!(action_for_signal(SIGUSR2), Some(HotkeyAction::PriceCheck));
        assert_eq!(action_for_signal(SIGUSR1), Some(HotkeyAction::PriceCheckPinned));
        assert_eq!(action_for_signal(signal_hook::consts::SIGTERM), None);
    }

    /// One test raises both signals: every receiver registered in this process sees each
    /// signal, so separate tests would observe each other's events.
    #[test]
    fn sigusr2_fires_price_check_and_sigusr1_pins() {
        let rx = sigusr2_trigger().unwrap();
        signal_hook::low_level::raise(SIGUSR2).unwrap();
        assert_eq!(rx.recv_timeout(Duration::from_secs(5)).unwrap().action, HotkeyAction::PriceCheck);
        signal_hook::low_level::raise(SIGUSR1).unwrap();
        assert_eq!(rx.recv_timeout(Duration::from_secs(5)).unwrap().action, HotkeyAction::PriceCheckPinned);
    }
}
