import { useState } from "react";
import type { KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import type { Chord, HotkeyAction } from "../../bindings";
import { commands } from "../../bindings";
import { cx } from "../../components/ui";
import { captureChord, formatChord, sameChord } from "../../lib/chord";

type Props = {
  action: HotkeyAction;
  chord: Chord;
  /** Chords bound to the other actions, to reject clashes before asking the backend. */
  others: Chord[];
  onBound: (chord: Chord) => void;
  error?: string;
};

export function HotkeyRecorder({ action, chord, others, onBound, error }: Props) {
  const { t } = useTranslation();
  const [recording, setRecording] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const label = t(`hotkeys.${action}`);
  const msgId = `hotkey-${action}-msg`;

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
        setBusy(true);
        const out = await commands.rebindHotkey(action, res.chord);
        setBusy(false);
        if (out.status === "ok") {
          setMessage(null);
          onBound(res.chord);
        } else {
          setMessage(t("hotkeys.rebindFailed", { message: out.error.message }));
        }
      }
    }
  };

  const shown = message ?? (error ? t(error) : null);

  return (
    <div className="flex flex-col gap-1 py-1.5">
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm" id={`hotkey-${action}-label`}>
          {label}
        </span>
        <button
          type="button"
          aria-labelledby={`hotkey-${action}-label hotkey-${action}-btn`}
          id={`hotkey-${action}-btn`}
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
          {recording ? t("hotkeys.pressKeys") : formatChord(chord)}
        </button>
      </div>
      {shown && (
        <p id={msgId} className="text-xs text-bad" role="alert">
          {shown}
        </p>
      )}
    </div>
  );
}
