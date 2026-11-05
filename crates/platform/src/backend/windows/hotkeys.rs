//! Global hotkeys via `global-hotkey`.
//!
//! The manager owns a hidden window that receives `WM_HOTKEY`, so it must be created and pumped by
//! one thread, and it is `!Send`. We give it a dedicated message-loop thread; trait calls are sent
//! as commands over a channel plus a `PostThreadMessageW` wake-up, and answered synchronously.
use std::collections::HashMap;
use std::sync::Mutex;
use std::sync::mpsc::{self, Receiver, Sender, SyncSender};
use std::time::Duration;

use global_hotkey::hotkey::HotKey;
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};
use windows::Win32::Foundation::{LPARAM, WPARAM};
use windows::Win32::System::Threading::GetCurrentThreadId;
use windows::Win32::UI::WindowsAndMessaging::{
    DispatchMessageW, GetMessageW, MSG, PostThreadMessageW, TranslateMessage, WM_APP, WM_QUIT,
};

use crate::backend::ghk::to_hotkey;
use crate::{Chord, HotkeyAction, HotkeyEvent, HotkeyHost, PlatformError};

const WM_RKL_CMD: u32 = WM_APP + 0x4B;
const REPLY_TIMEOUT: Duration = Duration::from_secs(2);

type Reply = SyncSender<Result<(), PlatformError>>;

enum Cmd {
    Register(HotkeyAction, HotKey, String, Reply),
    Unregister(HotkeyAction, Reply),
    UnregisterAll(Reply),
}

#[derive(Debug)]
pub(super) struct Win32HotkeyHost {
    thread_id: u32,
    cmds: Mutex<Sender<Cmd>>,
    events: Mutex<Option<Receiver<HotkeyEvent>>>,
}

impl std::fmt::Debug for Cmd {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Cmd::Register(..) => "Register",
            Cmd::Unregister(..) => "Unregister",
            Cmd::UnregisterAll(..) => "UnregisterAll",
        })
    }
}

impl Win32HotkeyHost {
    pub(super) fn spawn() -> Result<Self, PlatformError> {
        let (cmd_tx, cmd_rx) = mpsc::channel();
        let (ev_tx, ev_rx) = mpsc::channel();
        let (ready_tx, ready_rx) = mpsc::sync_channel(1);
        std::thread::Builder::new()
            .name("rkl-win-hotkeys".into())
            .spawn(move || hotkey_thread(&cmd_rx, &ev_tx, &ready_tx))
            .map_err(|e| PlatformError::Backend(format!("spawn hotkey thread: {e}")))?;
        let thread_id =
            ready_rx.recv().map_err(|_| PlatformError::Backend("hotkey thread exited during setup".into()))??;
        Ok(Self { thread_id, cmds: Mutex::new(cmd_tx), events: Mutex::new(Some(ev_rx)) })
    }

    fn call(&self, make: impl FnOnce(Reply) -> Cmd) -> Result<(), PlatformError> {
        let (reply_tx, reply_rx) = mpsc::sync_channel(1);
        let gone = || PlatformError::Backend("hotkey thread is not running".into());
        self.cmds.lock().map_err(|_| gone())?.send(make(reply_tx)).map_err(|_| gone())?;
        // SAFETY: posts a plain thread message with no pointer payload.
        unsafe { PostThreadMessageW(self.thread_id, WM_RKL_CMD, WPARAM(0), LPARAM(0)) }
            .map_err(|e| PlatformError::Backend(format!("PostThreadMessageW: {e}")))?;
        reply_rx.recv_timeout(REPLY_TIMEOUT).map_err(|_| PlatformError::Timeout(REPLY_TIMEOUT))?
    }
}

impl Drop for Win32HotkeyHost {
    fn drop(&mut self) {
        // SAFETY: posts WM_QUIT (no payload); the thread drops the manager, which destroys its
        // window and thereby every registration.
        let _ = unsafe { PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0)) };
    }
}

impl HotkeyHost for Win32HotkeyHost {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        let hotkey = to_hotkey(chord)?;
        self.call(|r| Cmd::Register(id, hotkey, chord.to_string(), r))
    }

    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        self.call(|r| Cmd::Unregister(id, r))
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        self.call(Cmd::UnregisterAll)
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        self.events
            .lock()
            .map_err(|_| PlatformError::Backend("hotkey receiver lock poisoned".into()))?
            .take()
            .ok_or(PlatformError::AlreadyStarted)
    }
}

fn map_err(chord: &str, err: global_hotkey::Error) -> PlatformError {
    match err {
        global_hotkey::Error::AlreadyRegistered(_) => PlatformError::HotkeyConflict(chord.to_owned()),
        global_hotkey::Error::FailedToRegister(msg) => PlatformError::InvalidChord(format!("{chord}: {msg}")),
        other => PlatformError::Backend(other.to_string()),
    }
}

fn handle(manager: &GlobalHotKeyManager, registered: &mut HashMap<HotkeyAction, HotKey>, cmd: Cmd) {
    match cmd {
        Cmd::Register(action, hotkey, chord, reply) => {
            let result = (|| {
                match registered.get(&action) {
                    Some(old) if *old == hotkey => return Ok(()),
                    Some(old) => {
                        manager.unregister(*old).map_err(|e| PlatformError::Backend(e.to_string()))?;
                        registered.remove(&action);
                    }
                    None => {}
                }
                if registered.values().any(|hk| *hk == hotkey) {
                    return Err(PlatformError::HotkeyConflict(chord.clone()));
                }
                manager.register(hotkey).map_err(|e| map_err(&chord, e))?;
                registered.insert(action, hotkey);
                Ok(())
            })();
            let _ = reply.send(result);
        }
        Cmd::Unregister(action, reply) => {
            let result = match registered.remove(&action) {
                Some(hk) => manager.unregister(hk).map_err(|e| PlatformError::Backend(e.to_string())),
                None => Ok(()),
            };
            let _ = reply.send(result);
        }
        Cmd::UnregisterAll(reply) => {
            let all: Vec<HotKey> = registered.drain().map(|(_, hk)| hk).collect();
            let _ = reply.send(manager.unregister_all(&all).map_err(|e| PlatformError::Backend(e.to_string())));
        }
    }
}

fn hotkey_thread(cmds: &Receiver<Cmd>, events: &Sender<HotkeyEvent>, ready: &SyncSender<Result<u32, PlatformError>>) {
    let manager = match GlobalHotKeyManager::new() {
        Ok(m) => m,
        Err(e) => {
            let _ = ready.send(Err(PlatformError::Backend(format!("GlobalHotKeyManager: {e}"))));
            return;
        }
    };
    // Creating the manager's window gave this thread a message queue, so PostThreadMessageW works.
    // SAFETY: no arguments.
    let _ = ready.send(Ok(unsafe { GetCurrentThreadId() }));

    let mut registered: HashMap<HotkeyAction, HotKey> = HashMap::new();
    let mut msg = MSG::default();
    // SAFETY: `msg` is a valid out-pointer; 0 (WM_QUIT) or -1 (error) ends the loop.
    while unsafe { GetMessageW(&mut msg, None, 0, 0) }.0 > 0 {
        if msg.message == WM_RKL_CMD && msg.hwnd.is_invalid() {
            while let Ok(cmd) = cmds.try_recv() {
                handle(&manager, &mut registered, cmd);
            }
            continue;
        }
        // SAFETY: `msg` was filled by GetMessageW; WM_HOTKEY is dispatched to the manager's window
        // proc, which pushes into global-hotkey's channel synchronously on this thread.
        unsafe {
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
        while let Ok(ev) = GlobalHotKeyEvent::receiver().try_recv() {
            if ev.state != HotKeyState::Pressed {
                continue;
            }
            if let Some((&action, _)) = registered.iter().find(|(_, hk)| hk.id() == ev.id) {
                let _ = events.send(HotkeyEvent { action });
            }
        }
    }
    drop(manager);
}

/// Used when the manager could not be created; `Capabilities::global_hotkeys` is false then.
#[derive(Debug)]
pub(super) struct UnavailableHotkeys;

impl HotkeyHost for UnavailableHotkeys {
    fn register(&self, _id: HotkeyAction, _chord: &Chord) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported("global_hotkeys"))
    }

    fn unregister(&self, _id: HotkeyAction) -> Result<(), PlatformError> {
        Ok(())
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        Ok(())
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        Err(PlatformError::Unsupported("global_hotkeys"))
    }
}
