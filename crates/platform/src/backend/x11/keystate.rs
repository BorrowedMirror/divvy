//! Physical key state on X11 (`QueryKeymap` + `GetModifierMapping`).
//!
//! Used before synthesising the copy chord:
//! - `global-hotkey` triggers on an `XGrabKey` passive grab, which turns into an active keyboard
//!   grab for as long as the hotkey's key is held. While that grab is active every key event,
//!   including our XTEST Ctrl+C, is delivered to the grabbing client (us), not the game. So we
//!   first wait (capped) until no non-modifier key is down.
//! - Modifiers still held (Ctrl from Ctrl+D) feed [`crate::backend::chord_plan`].
use std::time::{Duration, Instant};

use x11rb::connection::Connection as _;
use x11rb::protocol::xproto::ConnectionExt as _;
use x11rb::rust_connection::RustConnection;

use super::conn::x_err;
use crate::{KeyModifier, PlatformError};

/// `QueryKeymap` bit vector: bit `k` set = keycode `k` is down.
pub(crate) type Keymap = [u8; 32];

/// How long `wait_for_release` waits for the hotkey key to come up before sending anyway.
pub(crate) const RELEASE_WAIT: Duration = Duration::from_millis(400);
const RELEASE_POLL: Duration = Duration::from_millis(5);

pub(crate) fn is_down(keys: &Keymap, keycode: u8) -> bool {
    keys[usize::from(keycode / 8)] & (1 << (keycode % 8)) != 0
}

/// The server's modifier mapping: eight rows (Shift, Lock, Control, Mod1..Mod5) of keycodes.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub(crate) struct ModifierMap {
    rows: [Vec<u8>; 8],
}

/// Row of the modifier mapping for each modifier we model (Mod1 = Alt, Mod4 = Super: the
/// near-universal XKB defaults).
fn row(m: KeyModifier) -> usize {
    match m {
        KeyModifier::Shift => 0,
        KeyModifier::Ctrl => 2,
        KeyModifier::Alt => 3,
        KeyModifier::Meta => 6,
    }
}

const MODIFIERS: [KeyModifier; 4] = [KeyModifier::Ctrl, KeyModifier::Alt, KeyModifier::Shift, KeyModifier::Meta];

impl ModifierMap {
    /// From a `GetModifierMapping` reply (`keycodes.len() == 8 * keycodes_per_modifier`; 0 = unused).
    pub(crate) fn from_reply(keycodes: &[u8]) -> Self {
        let per = keycodes.len() / 8;
        let mut rows: [Vec<u8>; 8] = Default::default();
        if per > 0 {
            for (row, chunk) in rows.iter_mut().zip(keycodes.chunks(per)) {
                *row = chunk.iter().copied().filter(|&k| k != 0).collect();
            }
        }
        Self { rows }
    }

    /// Whether `keycode` is bound to any modifier (incl. Lock, NumLock, ISO_Level3...).
    pub(crate) fn is_modifier(&self, keycode: u8) -> bool {
        self.rows.iter().any(|r| r.contains(&keycode))
    }

    /// Keycodes of `m` that are down.
    pub(crate) fn held_keycodes(&self, m: KeyModifier, keys: &Keymap) -> Vec<u8> {
        self.rows[row(m)].iter().copied().filter(|&k| is_down(keys, k)).collect()
    }

    /// Modifiers with at least one key down.
    pub(crate) fn held(&self, keys: &Keymap) -> Vec<KeyModifier> {
        MODIFIERS.into_iter().filter(|&m| !self.held_keycodes(m, keys).is_empty()).collect()
    }

    /// Whether any key that is not a modifier is down (e.g. the `D` of Ctrl+D).
    pub(crate) fn non_modifier_down(&self, keys: &Keymap) -> bool {
        (8..=255u8).any(|k| is_down(keys, k) && !self.is_modifier(k))
    }
}

/// Poll `query` until no non-modifier key is down or `cap` elapsed; returns the last keymap.
/// `sleep` is injected for tests.
pub(crate) fn wait_for_release(
    map: &ModifierMap,
    cap: Duration,
    mut query: impl FnMut() -> Option<Keymap>,
    sleep: &dyn Fn(Duration),
) -> Option<Keymap> {
    let start = Instant::now();
    let mut waited = Duration::ZERO;
    loop {
        let keys = query()?;
        if !map.non_modifier_down(&keys) || waited >= cap || start.elapsed() >= cap {
            return Some(keys);
        }
        sleep(RELEASE_POLL);
        waited += RELEASE_POLL;
    }
}

/// What the simulator needs right before sending a chord.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub(crate) struct HeldState {
    pub(crate) modifiers: Vec<KeyModifier>,
    /// Held keycodes per modifier in `modifiers` (so the exact key, e.g. Shift_R, is released).
    pub(crate) keycodes: Vec<(KeyModifier, Vec<u8>)>,
}

impl HeldState {
    pub(crate) fn from_keymap(map: &ModifierMap, keys: &Keymap) -> Self {
        let modifiers = map.held(keys);
        let keycodes = modifiers.iter().map(|&m| (m, map.held_keycodes(m, keys))).collect();
        Self { modifiers, keycodes }
    }

    pub(crate) fn keycodes_of(&self, m: KeyModifier) -> &[u8] {
        self.keycodes.iter().find(|(k, _)| *k == m).map_or(&[], |(_, v)| v.as_slice())
    }
}

/// A private X connection for key-state queries.
pub(crate) struct X11KeyState {
    conn: RustConnection,
}

impl std::fmt::Debug for X11KeyState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("X11KeyState").finish_non_exhaustive()
    }
}

impl X11KeyState {
    pub(crate) fn connect(display: Option<&str>) -> Result<Self, PlatformError> {
        let (conn, _) = x11rb::connect(display).map_err(x_err)?;
        Ok(Self { conn })
    }

    fn modifier_map(&self) -> Option<ModifierMap> {
        let reply = self.conn.get_modifier_mapping().ok()?.reply().ok()?;
        Some(ModifierMap::from_reply(&reply.keycodes))
    }

    fn keymap(&self) -> Option<Keymap> {
        Some(self.conn.query_keymap().ok()?.reply().ok()?.keys)
    }

    /// Wait (up to `cap`) for the hotkey's non-modifier keys to come up, then report the held
    /// modifiers. `None` when the server cannot be queried (treated as "nothing held").
    pub(crate) fn settle(&self, cap: Duration) -> Option<HeldState> {
        let map = self.modifier_map()?;
        let keys = wait_for_release(&map, cap, || self.keymap(), &std::thread::sleep)?;
        let _ = self.conn.flush();
        Some(HeldState::from_keymap(&map, &keys))
    }
}

#[cfg(test)]
mod tests {
    use std::cell::Cell;

    use super::*;

    const SHIFT_L: u8 = 50;
    const SHIFT_R: u8 = 62;
    const CTRL_L: u8 = 37;
    const ALT_L: u8 = 64;
    const SUPER_L: u8 = 133;
    const NUM_LOCK: u8 = 77;
    const KEY_D: u8 = 40;

    fn map() -> ModifierMap {
        // keycodes_per_modifier = 2: Shift, Lock, Control, Mod1, Mod2, Mod3, Mod4, Mod5.
        ModifierMap::from_reply(&[SHIFT_L, SHIFT_R, 66, 0, CTRL_L, 105, ALT_L, 0, NUM_LOCK, 0, 0, 0, SUPER_L, 0, 92, 0])
    }

    fn keys(down: &[u8]) -> Keymap {
        let mut k = [0u8; 32];
        for &c in down {
            k[usize::from(c / 8)] |= 1 << (c % 8);
        }
        k
    }

    #[test]
    fn modifier_map_parses_rows() {
        let m = map();
        assert!(m.is_modifier(SHIFT_R) && m.is_modifier(NUM_LOCK) && m.is_modifier(92));
        assert!(!m.is_modifier(KEY_D));
        assert!(!m.is_modifier(0), "unused slots are dropped");
        assert_eq!(ModifierMap::from_reply(&[]), ModifierMap::default());
        assert!(is_down(&keys(&[255]), 255));
    }

    #[test]
    fn held_modifiers_and_keys() {
        let m = map();
        let k = keys(&[CTRL_L, SHIFT_R, KEY_D, NUM_LOCK]);
        assert_eq!(m.held(&k), vec![KeyModifier::Ctrl, KeyModifier::Shift]);
        assert_eq!(m.held_keycodes(KeyModifier::Shift, &k), vec![SHIFT_R]);
        assert!(m.non_modifier_down(&k));
        assert!(!m.non_modifier_down(&keys(&[CTRL_L, NUM_LOCK])));
        assert_eq!(m.held(&keys(&[ALT_L, SUPER_L])), vec![KeyModifier::Alt, KeyModifier::Meta]);

        let h = HeldState::from_keymap(&m, &k);
        assert_eq!(h.modifiers, vec![KeyModifier::Ctrl, KeyModifier::Shift]);
        assert_eq!(h.keycodes_of(KeyModifier::Shift), [SHIFT_R]);
        assert!(h.keycodes_of(KeyModifier::Alt).is_empty());
    }

    #[test]
    fn waits_until_the_hotkey_key_is_released() {
        let m = map();
        let polls = Cell::new(0);
        let got = wait_for_release(
            &m,
            Duration::from_secs(60),
            || {
                polls.set(polls.get() + 1);
                Some(if polls.get() < 4 { keys(&[CTRL_L, KEY_D]) } else { keys(&[CTRL_L]) })
            },
            &|d| assert_eq!(d, RELEASE_POLL),
        );
        assert_eq!(got, Some(keys(&[CTRL_L])));
        assert_eq!(polls.get(), 4);
    }

    #[test]
    fn wait_is_capped_and_tolerates_query_failure() {
        let m = map();
        let polls = Cell::new(0u32);
        let got = wait_for_release(
            &m,
            Duration::from_millis(20),
            || {
                polls.set(polls.get() + 1);
                Some(keys(&[KEY_D]))
            },
            &|_| {},
        );
        assert_eq!(got, Some(keys(&[KEY_D])), "sends anyway after the cap");
        assert_eq!(polls.get(), 5, "20ms / 5ms poll + initial query");
        assert_eq!(wait_for_release(&m, Duration::from_millis(20), || None, &|_| {}), None);
    }
}
