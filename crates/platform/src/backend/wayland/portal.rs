//! Global hotkeys through the XDG `GlobalShortcuts` portal (KDE, Hyprland, GNOME 48+...).
//! ashpd is async; it runs on a private current-thread tokio runtime on its own thread and
//! talks to the sync world through channels.
use std::collections::HashMap;
use std::sync::mpsc::{SyncSender, TrySendError};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Duration;

use ashpd::desktop::global_shortcuts::{GlobalShortcuts, NewShortcut};
use futures::StreamExt;
use tokio::sync::mpsc::{UnboundedSender, unbounded_channel};

use crate::{Chord, HotkeyAction, HotkeyEvent, KeyModifier, PlatformError};

/// How long `create()` waits for the portal session.
pub(crate) const CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
/// How long `register` waits for the portal (it may show a confirmation dialog first; after
/// this we return `Ok` and the binding completes in the background).
const BIND_WAIT: Duration = Duration::from_secs(5);

type Reply = std::sync::mpsc::SyncSender<Result<(), PlatformError>>;

enum Cmd {
    Bind(Vec<(HotkeyAction, Chord)>, Reply),
}

pub(crate) fn action_id(a: HotkeyAction) -> String {
    match a {
        HotkeyAction::PriceCheck => "price_check".into(),
        HotkeyAction::PriceCheckPinned => "price_check_pinned".into(),
        HotkeyAction::OpenSettings => "open_settings".into(),
        HotkeyAction::OpenTrade => "open_trade".into(),
        HotkeyAction::Chat(i) => format!("chat_{i}"),
        HotkeyAction::StashSearch(i) => format!("stash_search_{i}"),
    }
}

fn action_from_id(id: &str) -> Option<HotkeyAction> {
    let fixed =
        [HotkeyAction::PriceCheck, HotkeyAction::PriceCheckPinned, HotkeyAction::OpenSettings, HotkeyAction::OpenTrade];
    if let Some(a) = fixed.into_iter().find(|a| action_id(*a) == id) {
        return Some(a);
    }
    if let Some(i) = id.strip_prefix("stash_search_").and_then(|n| n.parse().ok()) {
        return Some(HotkeyAction::StashSearch(i));
    }
    id.strip_prefix("chat_").and_then(|n| n.parse().ok()).map(HotkeyAction::Chat)
}

fn description(a: HotkeyAction) -> String {
    match a {
        HotkeyAction::PriceCheck => "Price check the hovered item".into(),
        HotkeyAction::PriceCheckPinned => "Price check and pin the overlay".into(),
        HotkeyAction::OpenSettings => "Open RKL settings".into(),
        HotkeyAction::OpenTrade => "Open the last price check on the trade site".into(),
        HotkeyAction::Chat(i) => format!("Chat command {}", u16::from(i) + 1),
        HotkeyAction::StashSearch(i) => format!("Stash search {}", u16::from(i) + 1),
    }
}

/// Chord -> XDG shortcut trigger (`CTRL+ALT+d`, keysym names for the key).
pub(crate) fn trigger(chord: &Chord) -> String {
    let mut parts: Vec<String> = chord
        .modifiers
        .iter()
        .map(|m| {
            match m {
                KeyModifier::Ctrl => "CTRL",
                KeyModifier::Alt => "ALT",
                KeyModifier::Shift => "SHIFT",
                KeyModifier::Meta => "LOGO",
            }
            .to_owned()
        })
        .collect();
    let k = &chord.key;
    let key = if let Some(c) = k.strip_prefix("Key").filter(|c| c.len() == 1) {
        c.to_ascii_lowercase()
    } else if let Some(d) = k.strip_prefix("Digit").filter(|d| d.len() == 1) {
        d.to_owned()
    } else {
        match k.as_str() {
            "Enter" => "Return".to_owned(),
            "Space" => "space".to_owned(),
            other => other.to_owned(),
        }
    };
    parts.push(key);
    parts.join("+")
}

#[derive(Debug)]
pub(crate) struct PortalHotkeys {
    cmd: UnboundedSender<Cmd>,
    bound: Mutex<HashMap<HotkeyAction, Chord>>,
    enabled: Arc<Mutex<HashMap<HotkeyAction, bool>>>,
    /// Bind failures reported by the portal thread (also those after `register` returned).
    notes: Arc<Mutex<Vec<String>>>,
}

type Bindings = Vec<(HotkeyAction, Chord)>;
type BindFuture<'a> = std::pin::Pin<Box<dyn std::future::Future<Output = Result<(), PlatformError>> + 'a>>;

/// One `BindShortcuts` call for every binding in `list`.
fn bind<'a>(
    proxy: &'a GlobalShortcuts<'static>,
    session: &'a ashpd::desktop::Session<'static, GlobalShortcuts<'static>>,
    list: Vec<(HotkeyAction, Chord)>,
) -> BindFuture<'a> {
    Box::pin(async move {
        let shortcuts = new_shortcuts(&list);
        match proxy.bind_shortcuts(session, &shortcuts, None).await {
            Ok(req) => req.response().map(|_| ()).map_err(portal_err),
            Err(e) => Err(portal_err(e)),
        }
    })
}

fn new_shortcuts(list: &[(HotkeyAction, Chord)]) -> Vec<NewShortcut> {
    list.iter()
        .map(|(a, c)| NewShortcut::new(action_id(*a), description(*a)).preferred_trigger(Some(trigger(c).as_str())))
        .collect()
}

/// `PlatformError` is not `Clone`; replies to superseded requests get an equivalent error.
fn clone_result(r: &Result<(), PlatformError>) -> Result<(), PlatformError> {
    r.as_ref().copied().map_err(|e| PlatformError::Backend(e.to_string()))
}

impl std::fmt::Debug for Cmd {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("Cmd")
    }
}

fn lock<T>(m: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    m.lock().unwrap_or_else(PoisonError::into_inner)
}

fn portal_err(e: impl std::fmt::Display) -> PlatformError {
    PlatformError::Backend(format!("GlobalShortcuts portal: {e}"))
}

impl PortalHotkeys {
    /// Open a portal session; `Err` when no portal implements GlobalShortcuts.
    pub(crate) fn connect(events: SyncSender<HotkeyEvent>, timeout: Duration) -> Result<Self, PlatformError> {
        let (cmd_tx, mut cmd_rx) = unbounded_channel::<Cmd>();
        let (ready_tx, ready_rx) = std::sync::mpsc::sync_channel::<Result<(), PlatformError>>(1);
        let enabled: Arc<Mutex<HashMap<HotkeyAction, bool>>> = Arc::default();
        let enabled_thread = Arc::clone(&enabled);
        let notes: Arc<Mutex<Vec<String>>> = Arc::default();
        let notes_thread = Arc::clone(&notes);
        std::thread::Builder::new()
            .name("rkl-portal".into())
            .spawn(move || {
                let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                    Ok(rt) => rt,
                    Err(e) => {
                        let _ = ready_tx.send(Err(portal_err(e)));
                        return;
                    }
                };
                rt.block_on(async move {
                    let setup = async {
                        let proxy = GlobalShortcuts::new().await?;
                        let session = proxy.create_session().await?;
                        let activated = proxy.receive_activated().await?;
                        Ok::<_, ashpd::Error>((proxy, session, activated))
                    };
                    let (proxy, session, activated) = match setup.await {
                        Ok(v) => v,
                        Err(e) => {
                            let _ = ready_tx.send(Err(portal_err(e)));
                            return;
                        }
                    };
                    let _ = ready_tx.send(Ok(()));
                    let mut activated = Box::pin(activated);
                    // The bind in flight (it may wait for a user confirmation, so it must not block
                    // activations) and the replies waiting on it; plus the newest bind request
                    // queued behind it (each request carries every binding, so newer supersedes).
                    let mut in_flight: Option<BindFuture<'_>> = None;
                    let mut waiting: Vec<Reply> = Vec::new();
                    let mut queued: Option<(Bindings, Vec<Reply>)> = None;
                    loop {
                        tokio::select! {
                            Some(a) = activated.next() => {
                                let Some(action) = action_from_id(a.shortcut_id()) else { continue };
                                if !lock(&enabled_thread).get(&action).copied().unwrap_or(false) {
                                    continue;
                                }
                                if let Err(TrySendError::Disconnected(_)) = events.try_send(HotkeyEvent { action }) {
                                    break;
                                }
                            }
                            result = async {
                                match in_flight.as_mut() {
                                    Some(f) => f.await,
                                    None => std::future::pending().await,
                                }
                            },
                                if in_flight.is_some() =>
                            {
                                in_flight = None;
                                if let Err(e) = &result {
                                    tracing::warn!(error = %e, "portal bind failed");
                                    lock(&notes_thread).push(format!("Global shortcuts could not be bound: {e}"));
                                }
                                for reply in waiting.drain(..) {
                                    let _ = reply.try_send(clone_result(&result));
                                }
                                if let Some((list, replies)) = queued.take() {
                                    in_flight = Some(bind(&proxy, &session, list));
                                    waiting = replies;
                                }
                            }
                            cmd = cmd_rx.recv() => match cmd {
                                None => break,
                                Some(Cmd::Bind(list, reply)) => {
                                    if in_flight.is_none() {
                                        in_flight = Some(bind(&proxy, &session, list));
                                        waiting.push(reply);
                                    } else {
                                        let mut replies = queued.take().map(|(_, r)| r).unwrap_or_default();
                                        replies.push(reply);
                                        queued = Some((list, replies));
                                    }
                                }
                            },
                        }
                    }
                });
            })
            .map_err(|e| PlatformError::Backend(format!("spawn portal thread: {e}")))?;
        match ready_rx.recv_timeout(timeout) {
            Ok(Ok(())) => Ok(Self { cmd: cmd_tx, bound: Mutex::default(), enabled, notes }),
            Ok(Err(e)) => Err(e),
            Err(_) => Err(PlatformError::Timeout(timeout)),
        }
    }

    fn rebind(&self) -> Result<(), PlatformError> {
        let list: Vec<_> = lock(&self.bound).iter().map(|(a, c)| (*a, c.clone())).collect();
        let (tx, rx) = std::sync::mpsc::sync_channel(1);
        self.cmd.send(Cmd::Bind(list, tx)).map_err(|_| portal_err("portal thread stopped"))?;
        match rx.recv_timeout(BIND_WAIT) {
            Ok(r) => r,
            // Still waiting on the user (confirmation dialog); it completes in the background.
            Err(_) => Ok(()),
        }
    }
}

impl PortalHotkeys {
    pub(crate) fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        self.register_all(&[(id, chord.clone())])
    }

    /// Add/replace all `bindings` and send them in a single portal bind.
    pub(crate) fn register_all(&self, bindings: &[(HotkeyAction, Chord)]) -> Result<(), PlatformError> {
        for (id, chord) in bindings {
            lock(&self.bound).insert(*id, chord.clone());
            lock(&self.enabled).insert(*id, true);
        }
        self.rebind()
    }

    pub(crate) fn notes(&self) -> Vec<String> {
        lock(&self.notes).clone()
    }

    /// The portal cannot remove a single binding from a session; we stop forwarding it.
    pub(crate) fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        lock(&self.enabled).insert(id, false);
        Ok(())
    }

    pub(crate) fn unregister_all(&self) -> Result<(), PlatformError> {
        lock(&self.enabled).values_mut().for_each(|v| *v = false);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn triggers_follow_xdg_shortcut_syntax() {
        assert_eq!(trigger(&Chord::parse("Ctrl+Alt+D").unwrap()), "CTRL+ALT+d");
        assert_eq!(trigger(&Chord::parse("meta+shift+1").unwrap()), "LOGO+SHIFT+1");
        assert_eq!(trigger(&Chord::parse("ctrl+Enter").unwrap()), "CTRL+Return");
        assert_eq!(trigger(&Chord::parse("ctrl+Space").unwrap()), "CTRL+space");
        assert_eq!(trigger(&Chord::parse("F5").unwrap()), "F5");
    }

    #[test]
    fn shortcuts_carry_ids_and_triggers() {
        let list = [
            (HotkeyAction::PriceCheck, Chord::parse("ctrl+d").unwrap()),
            (HotkeyAction::OpenSettings, Chord::parse("ctrl+shift+F5").unwrap()),
        ];
        assert_eq!(new_shortcuts(&list).len(), 2);
        assert!(clone_result(&Ok(())).is_ok());
        let e = clone_result(&Err(PlatformError::Timeout(Duration::from_secs(1)))).unwrap_err();
        assert!(e.to_string().contains("timed out"));
    }

    #[test]
    fn ids_roundtrip() {
        for a in [
            HotkeyAction::PriceCheck,
            HotkeyAction::PriceCheckPinned,
            HotkeyAction::OpenSettings,
            HotkeyAction::OpenTrade,
        ] {
            assert_eq!(action_from_id(&action_id(a)), Some(a));
            assert!(!description(a).is_empty());
        }
        assert_eq!(action_from_id("nope"), None);
    }
}
