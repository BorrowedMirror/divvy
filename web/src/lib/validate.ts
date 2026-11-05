import type { HotkeyAction, Settings_Serialize } from "../bindings";
import { sameChord } from "./chord";

/** Field path -> i18n key of the validation message. */
export type ValidationErrors = Partial<Record<string, string>>;

export const HOTKEY_ACTIONS: HotkeyAction[] = ["price_check", "price_check_pinned", "open_settings"];

/** Mirror of `looks_like_email` in crates/config: visible ASCII, no `;(),<>`, dotted domain. */
export function looksLikeEmail(s: string): boolean {
  const at = s.indexOf("@");
  if (at <= 0) return false;
  const domain = s.slice(at + 1);
  if (domain.includes("@")) return false;
  if (![...s].every((c) => c >= "!" && c <= "~" && !";(),<>".includes(c))) return false;
  const labels = domain.split(".");
  return labels.length >= 2 && labels.every((l) => l.length > 0);
}

/** Mirror of the poesessid rule in crates/config: ASCII letters and digits only. */
export function looksLikeSessionId(s: string): boolean {
  return /^[A-Za-z0-9]*$/.test(s);
}

const inRange = (v: number | null, lo: number, hi: number) => v !== null && Number.isFinite(v) && v >= lo && v <= hi;

/**
 * Client-side mirror of `kalandra_config::validate` so messages appear inline; the backend stays
 * authoritative. Numeric fields are `f64`/`u64` in Rust, so `null` is invalid even though the
 * generated bindings allow it.
 */
export function validateSettings(s: Settings_Serialize): ValidationErrors {
  const errors: ValidationErrors = {};
  if (!s.league.auto_detect && !s.league.league?.trim()) errors["league.league"] = "validation.league";
  HOTKEY_ACTIONS.forEach((a, i) => {
    const clash = HOTKEY_ACTIONS.slice(0, i).find((b) => sameChord(s.hotkeys[a], s.hotkeys[b]));
    if (clash) errors[`hotkeys.${a}`] = "validation.hotkeyClash";
  });
  if (!inRange(s.trade.widen_pct, 0, 100)) errors["trade.widen_pct"] = "validation.widen";
  if (s.trade.contact !== null && !looksLikeEmail(s.trade.contact)) errors["trade.contact"] = "validation.email";
  if (s.trade.poesessid && !looksLikeSessionId(s.trade.poesessid)) errors["trade.poesessid"] = "validation.poesessid";
  if (!inRange(s.overlay.opacity, 0.1, 1)) errors["overlay.opacity"] = "validation.opacity";
  const ms = s.overlay.auto_hide_ms;
  if (!Number.isInteger(ms) || !inRange(ms, 0, 10_000)) errors["overlay.auto_hide_ms"] = "validation.autoHide";
  return errors;
}
