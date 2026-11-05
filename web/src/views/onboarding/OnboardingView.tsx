import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { PermissionInfo_Serialize, PermissionReport_Serialize } from "../../bindings";
import { Button, cx } from "../../components/ui";
import { keys, usePermissions, useRequestPermission, useSaveSettings, useSettingsQuery } from "../../hooks/queries";
import { toIpcError } from "../../lib/ipc";
import { ProtonMouseCard } from "../../components/ProtonMouseCard";

/** Example compositor bindings for `rkl --trigger` (code, not translated). */
export const TRIGGER_EXAMPLES = [
  { id: "sway", code: "bindsym Ctrl+d exec rkl --trigger" },
  { id: "hyprland", code: "bind = CTRL, D, exec, rkl --trigger" },
  { id: "kde", code: "rkl --trigger --pinned" },
] as const;

/** Permission kinds the backend can report; others fall back to a generic label. */
const KNOWN_PERMISSIONS = ["accessibility", "screen_recording", "input_monitoring"];

/** Permissions whose grant is only picked up after relaunching (see docs/research/macos.md). */
const NEEDS_RELAUNCH = ["screen_recording", "input_monitoring"];

export function OnboardingView({ onDone }: { onDone?: () => void }) {
  const { t } = useTranslation();
  const query = usePermissions();

  return (
    <div className="mx-auto min-h-screen max-w-2xl bg-bg p-6 text-fg">
      <header className="mb-4">
        <h1 className="text-lg font-semibold text-gold">{t("onboarding.title")}</h1>
        <p className="mt-1 text-sm text-muted">{t("onboarding.intro")}</p>
      </header>
      {query.isPending && <p className="text-muted">{t("common.loading")}</p>}
      {query.isError && (
        <p role="alert" className="text-bad">
          {t("onboarding.loadFailed")}
        </p>
      )}
      {query.data && <Steps report={query.data} />}
      {query.data && <Notes notes={query.data.notes} />}
      <Footer blocked={query.data ? isBlocked(query.data) : false} onDone={onDone} />
    </div>
  );
}

export function isBlocked(r: PermissionReport_Serialize): boolean {
  return r.missing.some((p) => p.required && !p.granted);
}

function Steps({ report }: { report: PermissionReport_Serialize }) {
  const { t } = useTranslation();
  const steps = (() => {
    switch (report.platform) {
      case "macos":
        return <MacosSteps permissions={report.missing} />;
      case "windows":
        return (
          <>
            <Step title={t("onboarding.windows.windowedTitle")} body={t("onboarding.windows.windowedBody")} />
            <Step title={t("onboarding.windows.elevatedTitle")} body={t("onboarding.windows.elevatedBody")} />
          </>
        );
      case "wayland":
        return <WaylandSteps gnome={report.gnome} />;
      default:
        return (
          <>
            {report.gnome ? (
              <Step title={t("onboarding.gnome.title")} body={t("onboarding.gnome.x11Body")} />
            ) : (
              <Step title={t("onboarding.x11.title")} body={t("onboarding.x11.body")} />
            )}
            <li className="list-none">
              <ProtonMouseCard />
            </li>
          </>
        );
    }
  })();
  return (
    <section aria-labelledby="onboarding-steps" className="flex flex-col gap-3">
      <h2 id="onboarding-steps" className="text-sm font-semibold text-muted">
        {t("onboarding.stepsHeading", { platform: t(`backend.${report.platform}`) })}
      </h2>
      <ol className="flex flex-col gap-3">{steps}</ol>
    </section>
  );
}

function Step({ title, body, children }: { title: string; body: string; children?: React.ReactNode }) {
  return (
    <li className="rounded-md border border-line bg-surface-2 p-3 text-sm">
      <h3 className="font-semibold">{title}</h3>
      <p className="mt-1 text-muted">{body}</p>
      {children}
    </li>
  );
}

function MacosSteps({ permissions }: { permissions: PermissionInfo_Serialize[] }) {
  const { t } = useTranslation();
  const request = useRequestPermission();
  const [opened, setOpened] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  const open = async (kind: string) => {
    setError(null);
    try {
      await request.mutateAsync(kind);
      setOpened((o) => (o.includes(kind) ? o : [...o, kind]));
    } catch (err) {
      setError(toIpcError(err).message);
    }
  };

  return (
    <>
      {permissions.map((p) => {
        const known = KNOWN_PERMISSIONS.includes(p.kind);
        const name = known ? t(`onboarding.permission.${p.kind}.name`) : t("onboarding.permission.unknown", { kind: p.kind });
        return (
          <li
            key={p.kind}
            data-kind={p.kind}
            data-granted={p.granted}
            className={cx(
              "rounded-md border p-3 text-sm",
              p.granted ? "border-good/50 bg-good/5" : "border-gold/40 bg-gold/5",
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <h3 className="flex items-center gap-2 font-semibold">
                <span aria-hidden="true" className={cx("w-4 text-center", p.granted ? "text-good" : "text-muted")}>
                  {p.granted ? "✓" : "○"}
                </span>
                {name}
                <span className="rounded bg-surface-3 px-1.5 py-0.5 text-[11px] font-normal text-muted">
                  {p.required ? t("onboarding.required") : t("onboarding.optional")}
                </span>
              </h3>
              <span className="text-xs" role="status">
                {p.granted ? t("onboarding.granted") : t("onboarding.notGranted")}
              </span>
            </div>
            {known && <p className="mt-1 text-muted">{t(`onboarding.permission.${p.kind}.body`)}</p>}
            {!p.granted && (
              <Button className="mt-2" onClick={() => void open(p.kind)} disabled={request.isPending}>
                {t("onboarding.openSettings")}
              </Button>
            )}
            {opened.includes(p.kind) && NEEDS_RELAUNCH.includes(p.kind) && (
              <p className="mt-2 text-xs text-muted">{t("onboarding.relaunch")}</p>
            )}
          </li>
        );
      })}
      {error && (
        <li role="alert" className="text-sm text-bad">
          {t("onboarding.requestFailed", { message: error })}
        </li>
      )}
    </>
  );
}

function WaylandSteps({ gnome }: { gnome: boolean }) {
  const { t } = useTranslation();
  return (
    <>
      {gnome && <Step title={t("onboarding.gnome.title")} body={t("onboarding.gnome.waylandBody")} />}
      <Step title={t("onboarding.wayland.degradedTitle")} body={t("onboarding.wayland.degradedBody")}>
        <ul className="mt-2 list-disc pl-5 text-muted">
          <li>{t("onboarding.wayland.portal")}</li>
          <li>{t("onboarding.wayland.layerShell")}</li>
          <li>{t("onboarding.wayland.noTracking")}</li>
        </ul>
      </Step>
      <Step title={t("onboarding.wayland.triggerTitle")} body={t("onboarding.wayland.triggerBody")}>
        <dl className="mt-2 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1">
          {TRIGGER_EXAMPLES.map((ex) => (
            <div key={ex.id} className="contents">
              <dt className="text-muted">{t(`onboarding.wayland.example.${ex.id}`)}</dt>
              <dd>
                <code className="rounded bg-surface-3 px-1 font-mono text-xs">{ex.code}</code>
              </dd>
            </div>
          ))}
        </dl>
        <p className="mt-2 text-xs text-muted">{t("onboarding.wayland.pinnedHint")}</p>
      </Step>
    </>
  );
}

function Notes({ notes }: { notes: string[] }) {
  const { t } = useTranslation();
  if (notes.length === 0) return null;
  return (
    <details className="mt-4 text-xs text-muted">
      <summary className="cursor-pointer">{t("onboarding.details")}</summary>
      <ul className="mt-1 list-disc pl-5">
        {notes.map((n) => (
          <li key={n}>{n}</li>
        ))}
      </ul>
    </details>
  );
}

function Footer({ blocked, onDone }: { blocked: boolean; onDone?: () => void }) {
  const { t } = useTranslation();
  const settings = useSettingsQuery().data;
  const save = useSaveSettings();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const done = async () => {
    if (!settings) return;
    setError(null);
    try {
      const saved = await save.mutateAsync({ ...settings, ui: { ...settings.ui, onboarding_done: true } });
      queryClient.setQueryData(keys.settings, saved);
      onDone?.();
    } catch (err) {
      setError(toIpcError(err).message);
    }
  };

  return (
    <footer className="mt-6 flex flex-col items-end gap-2">
      {blocked && <p className="text-sm text-muted">{t("onboarding.blockedHint")}</p>}
      {error && (
        <p role="alert" className="text-sm text-bad">
          {t("onboarding.saveFailed", { message: error })}
        </p>
      )}
      <Button variant="primary" onClick={() => void done()} disabled={!settings || save.isPending}>
        {blocked ? t("onboarding.continueWithout") : t("onboarding.done")}
      </Button>
    </footer>
  );
}
