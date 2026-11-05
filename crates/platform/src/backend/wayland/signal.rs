//! SIGUSR2 -> `PriceCheck`: the hotkey fallback when no GlobalShortcuts portal exists.
//! Users bind `pkill -USR2 kalandra` (or `kalandra --trigger`) in their compositor config.
use std::sync::mpsc::{Receiver, SyncSender, TrySendError, sync_channel};

use signal_hook::consts::SIGUSR2;
use signal_hook::iterator::Signals;

use crate::{HotkeyAction, HotkeyEvent, PlatformError};

/// Start listening for SIGUSR2; each signal yields one `PriceCheck` event.
pub fn sigusr2_trigger() -> Result<Receiver<HotkeyEvent>, PlatformError> {
    let (tx, rx) = sync_channel(16);
    spawn_sigusr2(tx)?;
    Ok(rx)
}

pub(crate) fn spawn_sigusr2(tx: SyncSender<HotkeyEvent>) -> Result<(), PlatformError> {
    let mut signals = Signals::new([SIGUSR2]).map_err(|e| PlatformError::Backend(format!("SIGUSR2 handler: {e}")))?;
    std::thread::Builder::new()
        .name("kalandra-sigusr2".into())
        .spawn(move || {
            for _ in signals.forever() {
                match tx.try_send(HotkeyEvent { action: HotkeyAction::PriceCheck }) {
                    Ok(()) | Err(TrySendError::Full(_)) => {}
                    Err(TrySendError::Disconnected(_)) => break,
                }
            }
        })
        .map_err(|e| PlatformError::Backend(format!("spawn SIGUSR2 thread: {e}")))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::*;

    #[test]
    fn sigusr2_fires_price_check() {
        let rx = sigusr2_trigger().unwrap();
        signal_hook::low_level::raise(SIGUSR2).unwrap();
        let ev = rx.recv_timeout(Duration::from_secs(5)).unwrap();
        assert_eq!(ev.action, HotkeyAction::PriceCheck);
    }
}
