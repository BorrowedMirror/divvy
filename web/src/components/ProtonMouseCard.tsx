import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { commands } from "../bindings";
import { useCommand } from "../hooks/useCommand";
import { Button } from "./ui";

export const protonMouseKey = ["protonMouse"] as const;

/**
 * Linux/Proton: Wine's fullscreen mouse capture stops clicks reaching the panel. Shows the state
 * of the game's Wine option and lets the user turn capture off (and undo it). Renders nothing
 * where it does not apply (no PoE2 Proton prefix, other OSes).
 */
export function ProtonMouseCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const command = useCommand();
  const [changed, setChanged] = useState(false);
  const [busy, setBusy] = useState(false);
  const status = useQuery({
    queryKey: protonMouseKey,
    queryFn: () => commands.getProtonMouseStatus(),
    refetchInterval: 3000,
    staleTime: 0,
  });
  const s = status.data;
  if (!s?.supported) return null;

  const set = async (disabled: boolean) => {
    setBusy(true);
    const res = await command(commands.setProtonMouseCapture(disabled));
    setBusy(false);
    if (res.status === "ok") {
      queryClient.setQueryData(protonMouseKey, res.data);
      setChanged(true);
    }
  };

  return (
    <section data-testid="proton-mouse" aria-labelledby="proton-heading" className="mt-6 rounded-md border border-line bg-surface-2 p-3 text-sm">
      <h2 id="proton-heading" className="font-semibold">
        {t("proton.title")}
      </h2>
      <p className="mt-1 text-muted">{t("proton.body")}</p>
      <p className="mt-2" data-testid="proton-status">
        {s.capture_disabled ? t("proton.statusOff") : t("proton.statusOn")}
      </p>
      {s.game_running && (
        <p role="status" className="mt-2 text-bad">
          {t("proton.quitGame")}
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        {!s.capture_disabled && (
          <Button variant="primary" disabled={busy || s.game_running} onClick={() => void set(true)}>
            {t("proton.fix")}
          </Button>
        )}
        {s.can_restore && (
          <Button disabled={busy || s.game_running} onClick={() => void set(false)}>
            {t("proton.undo")}
          </Button>
        )}
      </div>
      {changed && (
        <p role="status" className="mt-2 text-good">
          {t("proton.restartGame")}
        </p>
      )}
    </section>
  );
}
