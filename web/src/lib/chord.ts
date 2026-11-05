import type { Chord, KeyModifier } from "../bindings";

const MODIFIER_CODES = new Set([
  "ControlLeft",
  "ControlRight",
  "AltLeft",
  "AltRight",
  "ShiftLeft",
  "ShiftRight",
  "MetaLeft",
  "MetaRight",
  "OSLeft",
  "OSRight",
]);

export type ChordCapture =
  | { kind: "chord"; chord: Chord }
  /** Only modifiers held so far; keep listening. */
  | { kind: "pending" }
  | { kind: "cancel" }
  | { kind: "needs_modifier"; key: string };

type KeyLike = Pick<KeyboardEvent, "code" | "ctrlKey" | "altKey" | "shiftKey" | "metaKey">;

export function isFunctionKey(code: string): boolean {
  return /^F([1-9]|1\d|2[0-4])$/.test(code);
}

export function modifiersOf(e: KeyLike): KeyModifier[] {
  const mods: KeyModifier[] = [];
  if (e.ctrlKey) mods.push("ctrl");
  if (e.altKey) mods.push("alt");
  if (e.shiftKey) mods.push("shift");
  if (e.metaKey) mods.push("meta");
  return mods;
}

/** Interpret a keydown while recording a hotkey. Uses W3C `KeyboardEvent.code` names. */
export function captureChord(e: KeyLike): ChordCapture {
  if (!e.code || MODIFIER_CODES.has(e.code)) return { kind: "pending" };
  const modifiers = modifiersOf(e);
  if (e.code === "Escape" && modifiers.length === 0) return { kind: "cancel" };
  if (modifiers.length === 0 && !isFunctionKey(e.code)) return { kind: "needs_modifier", key: e.code };
  return { kind: "chord", chord: { modifiers, key: e.code } };
}

const MOD_ORDER: KeyModifier[] = ["ctrl", "alt", "shift", "meta"];
const MOD_LABEL: Record<KeyModifier, string> = { ctrl: "Ctrl", alt: "Alt", shift: "Shift", meta: "Meta" };

export function keyLabel(code: string): string {
  const m = /^(?:Key|Digit)(.)$/.exec(code);
  if (m?.[1]) return m[1];
  if (code.startsWith("Numpad")) return `Num${code.slice(6)}`;
  if (code.startsWith("Arrow")) return code.slice(5);
  return code;
}

export function formatChord(chord: Chord): string {
  const mods = MOD_ORDER.filter((m) => chord.modifiers.includes(m)).map((m) => MOD_LABEL[m]);
  return [...mods, keyLabel(chord.key)].join("+");
}

export function sameChord(a: Chord, b: Chord): boolean {
  if (a.key !== b.key || a.modifiers.length !== b.modifiers.length) return false;
  return a.modifiers.every((m) => b.modifiers.includes(m));
}
