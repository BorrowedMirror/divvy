import { useTranslation } from "react-i18next";

import { Button } from "@components/ui";
import { useBackendEvent } from "@hooks/useBackendEvent";
import { useCommand } from "@hooks/useCommand";
import { commands, events } from "@src/bindings";
import { useUpdateStore } from "@stores/update";

/** Records `update-available` for this window. Mounted once in `AppProviders`. */
export function UpdateSync() {
  const announce = useUpdateStore((s) => s.announce);
  useBackendEvent(events.updateAvailable, announce);
  return null;
}

/** "Version X is available" banner with an install-and-restart action. */
export function UpdateBanner() {
  const { t } = useTranslation();
  const run = useCommand();
  const { available, dismissed, installing, dismiss, setInstalling } = useUpdateStore();
  if (!available || available.version === dismissed) return null;

  const install = async () => {
    setInstalling(true);
    // On success the app restarts (or the Windows installer takes over), so this returns only on error.
    const res = await run(commands.installUpdate());
    if (res.status === "error") setInstalling(false);
  };

  return (
    <section
      aria-label={t("update.title")}
      className="mx-auto mt-4 flex max-w-2xl flex-col gap-2 rounded-md border border-gold/40 bg-surface-2 px-4 py-3 text-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span role="status" className="font-medium">
          {t("update.available", { version: available.version })}
        </span>
        <div className="flex gap-2">
          <Button onClick={dismiss} disabled={installing}>
            {t("update.later")}
          </Button>
          <Button variant="primary" onClick={() => void install()} disabled={installing}>
            {installing ? t("update.installing") : t("update.install")}
          </Button>
        </div>
      </div>
      {available.notes && (
        <details className="text-xs text-muted">
          <summary className="cursor-pointer">{t("update.notes")}</summary>
          <p className="mt-1 whitespace-pre-wrap">{available.notes}</p>
        </details>
      )}
    </section>
  );
}
