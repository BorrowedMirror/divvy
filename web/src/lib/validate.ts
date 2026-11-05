import type { HotkeyAction, Settings_Serialize } from "../bindings";
import { sameChord } from "./chord";

/** Field path -> i18n key of the validation message. */
export type ValidationErrors = Partial<Record<string, string>>;

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
export const HOTKEY_ACTIONS: HotkeyAction[] = ["price_check", "price_check_pinned", "open_settings"];

/** Client-side mirror of `kalandra_config::validate`; the backend stays authoritative. */
export function validateSettings(s: Settings_Serialize): ValidationErrors {
  const errors: ValidationErrors = {};
  const widen = s.trade.widen_pct;
  if (widen !== null && (!Number.isFinite(widen) || widen < 0 || widen > 100)) {
    errors["trade.widen_pct"] = "validation.widen";
  }
  const contact = s.trade.contact?.trim();
  if (contact && !EMAIL.test(contact)) errors["trade.contact"] = "validation.email";
  if (!s.league.auto_detect && !s.league.league) errors["league.league"] = "validation.league";
  const ms = s.overlay.auto_hide_ms;
  if (!Number.isInteger(ms) || ms < 0 || ms > 600_000) errors["overlay.auto_hide_ms"] = "validation.autoHide";
  const op = s.overlay.opacity;
  if (op !== null && (op < 0.2 || op > 1)) errors["overlay.opacity"] = "validation.opacity";
  HOTKEY_ACTIONS.forEach((a, i) => {
    const clash = HOTKEY_ACTIONS.slice(0, i).find((b) => sameChord(s.hotkeys[a], s.hotkeys[b]));
    if (clash) errors[`hotkeys.${a}`] = "validation.hotkeyClash";
  });
  return errors;
}
