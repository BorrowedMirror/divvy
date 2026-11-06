import { useState } from "react";
import type { KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import type { Chord, HotkeyAction } from "../../bindings";
import { commands } from "../../bindings";
import { cx } from "../../components/ui";
import { captureChord, formatChord, isUnbound, sameChord, UNBOUND } from "../../lib/chord";

type Props = {
  action: HotkeyAction;
  chord: Chord;
  /** Chords bound to the other actions, to reject clashes before asking the backend. */
  others: Chord[];
  onBound: (chord: Chord) => void;
  error?: string;
  /** Shown name (default: the action's translated name). */
  label?: string;
  /** DOM id stem (default: the action name). */
  id?: string;
  /** Only edit the draft (saved with the Save button) instead of rebinding right away. */
  draft?: boolean;
};

export function HotkeyRecorder({ action, chord, others, onBound, error, label: labelProp, id, draft }: Props) {
  const { t } = useTranslation();
  const [recording, setRecording] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const stem = id ?? (typeof action === "string" ? action : JSON.stringify(action).replace(/\W+/g, "-"));
  const label = labelProp ?? t(`hotkeys.${typeof action === "string" ? action : "price_check"}`);
  const msgId = `hotkey-${stem}-msg`;

  const onKeyDown = async (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!recording) return;
    e.preventDefault();
    e.stopPropagation();
    const res = captureChord(e);
    switch (res.kind) {
      case "pending":
        return;
      case "cancel":
        setRecording(false);
        setMessage(null);
        return;
      case "needs_modifier":
        setMessage(t("hotkeys.needsModifier"));
        return;
      case "chord": {
        setRecording(false);
        if (others.some((o) => sameChord(o, res.chord))) {
          setMessage(t("validation.hotkeyClash"));
          return;
        }
        await bind(res.chord);
      }
    }
  };

  const bind = async (next: Chord) => {
    if (draft) {
      setMessage(null);
      onBound(next);
      return;
    }
    setBusy(true);
    const out = await commands.rebindHotkey(action, next);
    setBusy(false);
    if (out.status === "ok") {
      setMessage(null);
      onBound(next);
    } else {
      setMessage(t("hotkeys.rebindFailed", { message: out.error.message }));
    }
  };

  const shown = message ?? (error ? t(error) : null);
  const unbound = isUnbound(chord);

  return (
    <div className="flex flex-col gap-1 py-1.5">
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm" id={`hotkey-${stem}-label`}>
          {label}
        </span>
        <span className="flex items-center gap-1">
        <button
          type="button"
          aria-labelledby={`hotkey-${stem}-label hotkey-${stem}-btn`}
          id={`hotkey-${stem}-btn`}
          aria-describedby={shown ? msgId : undefined}
          aria-pressed={recording}
          disabled={busy}
          onClick={() => {
            setRecording((r) => !r);
            setMessage(null);
          }}
          onKeyDown={(e) => void onKeyDown(e)}
          onBlur={() => setRecording(false)}
          className={cx(
            "min-w-36 rounded-md border px-3 py-1.5 font-mono text-sm",
            "outline-none focus-visible:ring-2 focus-visible:ring-gold",
            recording ? "animate-pulse border-gold bg-gold/10 text-gold" : "border-line bg-surface-2",
          )}
        >
          {recording ? t("hotkeys.pressKeys") : unbound ? t("hotkeys.notSet") : formatChord(chord)}
        </button>
        <button
          type="button"
          aria-label={t("hotkeys.clearAria", { action: label })}
          disabled={busy || unbound}
          onClick={() => void bind(UNBOUND)}
          className={cx(
            "rounded-md px-2 py-1.5 text-xs text-muted hover:text-fg disabled:cursor-not-allowed disabled:opacity-40",
            "outline-none focus-visible:ring-2 focus-visible:ring-gold",
          )}
        >
          {t("hotkeys.clear")}
        </button>
        </span>
      </div>
      {shown && (
        <p id={msgId} className="text-xs text-bad" role="alert">
          {shown}
        </p>
      )}
    </div>
  );
}
