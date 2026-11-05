import { Toast } from "@base-ui/react/toast";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { Capabilities, Diagnostics_Serialize } from "../../bindings";
import { Button, cx } from "../../components/ui";
import { ProtonMouseCard } from "../../components/ProtonMouseCard";
import { useDiagnostics } from "../../hooks/queries";

export const CAPABILITY_KEYS = [
  "window_tracking",
  "overlay_always_on_top",
  "overlay_click_through",
  "global_hotkeys",
  "passive_hook",
  "keystroke_injection",
  "clipboard",
] as const satisfies readonly (keyof Capabilities)[];

/** Guidance paragraphs (i18n keys) for the current platform state. */
export function guidanceFor(d: Diagnostics_Serialize): string[] {
  const out: string[] = [];
  const missing = d.capabilities.missing_permissions;
  if (missing.includes("accessibility")) out.push("guidance.accessibility");
  if (missing.includes("screen_recording")) out.push("guidance.screenRecording");
  for (const p of missing) {
    if (p !== "accessibility" && p !== "screen_recording") out.push(`permission:${p}`);
  }
  if (d.backend === "wayland") out.push("guidance.wayland");
  if (d.backend === "x11" && d.os === "linux") out.push("guidance.gnome");
  return out;
}

export function buildReport(d: Diagnostics_Serialize, t: TFunction): string {
  const lines = [
    `Rust King Ledger ${d.version}`,
    `${t("diagnostics.os")}: ${d.os}`,
    `${t("diagnostics.backend")}: ${d.backend}`,
    `${t("diagnostics.dataVersion")}: ${d.data_version ?? "-"}`,
    `${t("diagnostics.league")}: ${d.league ?? "-"}`,
    `${t("diagnostics.settingsPath")}: ${d.settings_path ?? "-"}`,
    `${t("diagnostics.logPath")}: ${d.log_path ?? "-"}`,
    "",
    `${t("diagnostics.capabilities")}:`,
    ...CAPABILITY_KEYS.map((k) => `  [${d.capabilities[k] ? "x" : " "}] ${k}`),
    `${t("diagnostics.missingPermissions")}: ${d.capabilities.missing_permissions.join(", ") || "-"}`,
    ...d.capabilities.notes.map((n) => `  note: ${n}`),
    "",
    `${t("diagnostics.recentErrors")}:`,
    ...(d.recent_errors.length ? d.recent_errors.map((e) => `  ${e}`) : ["  -"]),
  ];
  return lines.join("\n");
}

export function DiagnosticsView() {
  const { t } = useTranslation();
  const query = useDiagnostics();
  const toast = Toast.useToastManager();

  if (query.isPending) return <Page><p className="text-muted">{t("common.loading")}</p></Page>;
  if (query.isError) {
    return (
      <Page>
        <p role="alert" className="text-bad">
          {t("diagnostics.loadFailed")}
        </p>
      </Page>
    );
  }
  const d = query.data;
  const guidance = guidanceFor(d);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(buildReport(d, t));
      toast.add({ title: t("diagnostics.copied"), timeout: 2000 });
    } catch {
      toast.add({ title: t("diagnostics.copyFailed"), timeout: 4000 });
    }
  };

  return (
    <Page>
      <header className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-gold">{t("diagnostics.title")}</h1>
        <span className="flex items-center gap-3">
          <a href="#/onboarding" className="text-sm text-muted underline hover:text-fg">
            {t("diagnostics.onboardingLink")}
          </a>
          <Button variant="primary" onClick={() => void copy()}>
            {t("diagnostics.copyReport")}
          </Button>
        </span>
      </header>

      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
        <Fact label={t("diagnostics.version")} value={d.version} />
        <Fact label={t("diagnostics.os")} value={d.os} />
        <Fact label={t("diagnostics.backend")} value={t(`backend.${d.backend}`)} />
        <Fact label={t("diagnostics.dataVersion")} value={d.data_version} />
        <Fact label={t("diagnostics.league")} value={d.league} />
        <Fact label={t("diagnostics.settingsPath")} value={d.settings_path} mono />
        <Fact label={t("diagnostics.logPath")} value={d.log_path} mono />
      </dl>

      <section className="mt-6" aria-labelledby="caps-heading">
        <h2 id="caps-heading" className="mb-2 text-sm font-semibold text-muted">
          {t("diagnostics.capabilities")}
        </h2>
        <ul className="grid grid-cols-1 gap-1 sm:grid-cols-2">
          {CAPABILITY_KEYS.map((k) => {
            const ok = d.capabilities[k];
            return (
              <li key={k} className="flex items-center gap-2 text-sm" data-ok={ok}>
                <span aria-hidden="true" className={cx("w-4 text-center font-bold", ok ? "text-good" : "text-bad")}>
                  {ok ? "✓" : "✗"}
                </span>
                <span>{t(`capability.${k}`)}</span>
                <span className="sr-only">{ok ? t("diagnostics.supported") : t("diagnostics.unsupported")}</span>
              </li>
            );
          })}
        </ul>
        {d.capabilities.notes.length > 0 && (
          <ul className="mt-2 list-disc pl-5 text-xs text-muted">
            {d.capabilities.notes.map((n) => (
              <li key={n}>{n}</li>
            ))}
          </ul>
        )}
      </section>

      {guidance.length > 0 && (
        <section className="mt-6" aria-labelledby="guidance-heading">
          <h2 id="guidance-heading" className="mb-2 text-sm font-semibold text-muted">
            {t("diagnostics.missingPermissions")}
          </h2>
          <ul className="flex flex-col gap-2">
            {guidance.map((g) => (
              <li key={g} className="rounded-md border border-gold/40 bg-gold/5 p-2.5 text-sm">
                {g.startsWith("permission:")
                  ? t("guidance.generic", { permission: g.slice("permission:".length) })
                  : t(g)}
              </li>
            ))}
          </ul>
        </section>
      )}

      <ProtonMouseCard />

      <section className="mt-6" aria-labelledby="errors-heading">
        <h2 id="errors-heading" className="mb-2 text-sm font-semibold text-muted">
          {t("diagnostics.recentErrors")}
        </h2>
        {d.recent_errors.length === 0 ? (
          <p className="text-sm text-muted">{t("diagnostics.noErrors")}</p>
        ) : (
          <ul className="max-h-48 overflow-auto rounded-md border border-line bg-surface-2 p-2 font-mono text-xs">
            {d.recent_errors.map((e, i) => (
              <li key={`${i}-${e}`}>{e}</li>
            ))}
          </ul>
        )}
      </section>
    </Page>
  );
}

function Page({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto min-h-screen max-w-2xl bg-bg p-6 text-fg">{children}</div>;
}

function Fact({ label, value, mono }: { label: string; value: string | null; mono?: boolean }) {
  return (
    <>
      <dt className="text-muted">{label}</dt>
      <dd className={cx("break-all", mono && "font-mono text-xs leading-5")}>{value ?? "—"}</dd>
    </>
  );
}
