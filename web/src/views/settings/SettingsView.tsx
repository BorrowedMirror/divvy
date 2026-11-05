import { Tabs } from "@base-ui/react/tabs";
import { Toast } from "@base-ui/react/toast";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type {
  BackendKind,
  HotkeyAction,
  OverlaySide,
  PriceSourceKind,
  Settings_Serialize,
  Theme,
} from "../../bindings";
import { events } from "../../bindings";
import { Button, NumberInput, SelectField, SwitchField, cx } from "../../components/ui";
import { keys, useLeagues, useSaveSettings, useSettingsQuery } from "../../hooks/queries";
import { useTauriEvent } from "../../hooks/useTauriEvent";
import { LANGUAGES } from "../../i18n";
import { toIpcError } from "../../lib/ipc";
import { HOTKEY_ACTIONS, type ValidationErrors, validateSettings } from "../../lib/validate";
import { isDirty, useSettingsStore } from "../../stores/settings";
import { HotkeyRecorder } from "./HotkeyRecorder";

type Update = (fn: (s: Settings_Serialize) => Settings_Serialize) => void;
type TabProps = { s: Settings_Serialize; update: Update; errors: ValidationErrors };

const TABS = ["general", "hotkeys", "trade", "overlay", "platform"] as const;

export function SettingsView({ onOpenDiagnostics }: { onOpenDiagnostics?: () => void }) {
  const { t } = useTranslation();
  const query = useSettingsQuery();
  const { draft, base, externalChange, load, external, update, discard } = useSettingsStore();
  const save = useSaveSettings();
  const queryClient = useQueryClient();
  const toast = Toast.useToastManager();
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (query.data && useSettingsStore.getState().base === null) load(query.data);
  }, [query.data, load]);
  useTauriEvent(events.settingsChanged, (p) => external(p.settings));

  const errors = useMemo(() => (draft ? validateSettings(draft) : {}), [draft]);
  const invalid = Object.keys(errors).length > 0;
  const dirty = isDirty({ base, draft });

  if (query.isError) {
    return (
      <Shell>
        <p role="alert" className="text-bad">
          {t("settings.loadFailed")}
        </p>
      </Shell>
    );
  }
  if (!draft) {
    return (
      <Shell>
        <p className="text-muted">{t("common.loading")}</p>
      </Shell>
    );
  }

  const onSave = async () => {
    setSaveError(null);
    try {
      const saved = await save.mutateAsync(draft);
      load(saved);
      queryClient.setQueryData(keys.settings, saved);
      toast.add({ title: t("settings.saved"), timeout: 2500 });
    } catch (err) {
      setSaveError(toIpcError(err).message);
    }
  };

  return (
    <Shell>
      <header className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-gold">{t("settings.title")}</h1>
        {onOpenDiagnostics && (
          <button type="button" className="text-sm text-muted underline hover:text-fg" onClick={onOpenDiagnostics}>
            {t("diagnostics.title")}
          </button>
        )}
      </header>
      {externalChange && (
        <div role="status" className="mb-3 flex items-center justify-between rounded-md border border-gold/40 bg-gold/10 p-2 text-sm">
          {t("settings.externalChange")}
          <Button onClick={discard}>{t("settings.reload")}</Button>
        </div>
      )}
      <Tabs.Root defaultValue="general" className="flex flex-col gap-4">
        <Tabs.List className="flex gap-1 border-b border-line" aria-label={t("settings.title")}>
          {TABS.map((tab) => (
            <Tabs.Tab
              key={tab}
              value={tab}
              className={cx(
                "-mb-px border-b-2 border-transparent px-3 py-1.5 text-sm text-muted",
                "hover:text-fg data-[active]:border-gold data-[active]:text-gold",
                "outline-none focus-visible:ring-2 focus-visible:ring-gold",
              )}
            >
              {t(`settings.tabs.${tab}`)}
            </Tabs.Tab>
          ))}
        </Tabs.List>
        <Tabs.Panel value="general">
          <GeneralTab s={draft} update={update} errors={errors} />
        </Tabs.Panel>
        <Tabs.Panel value="hotkeys">
          <HotkeysTab s={draft} update={update} errors={errors} />
        </Tabs.Panel>
        <Tabs.Panel value="trade">
          <TradeTab s={draft} update={update} errors={errors} />
        </Tabs.Panel>
        <Tabs.Panel value="overlay">
          <OverlayTab s={draft} update={update} errors={errors} />
        </Tabs.Panel>
        <Tabs.Panel value="platform">
          <PlatformTab s={draft} update={update} errors={errors} />
        </Tabs.Panel>
      </Tabs.Root>
      <footer className="mt-6 flex items-center justify-end gap-2 border-t border-line pt-4">
        {invalid && (
          <p className="mr-auto text-sm text-bad" role="alert">
            {t("settings.fixErrors")}
          </p>
        )}
        {saveError && (
          <p className="mr-auto text-sm text-bad" role="alert">
            {t("settings.saveFailed", { message: saveError })}
          </p>
        )}
        <Button onClick={discard} disabled={!dirty || save.isPending}>
          {t("settings.discard")}
        </Button>
        <Button variant="primary" onClick={() => void onSave()} disabled={!dirty || invalid || save.isPending}>
          {save.isPending ? t("settings.saving") : t("settings.save")}
        </Button>
      </footer>
    </Shell>
  );
}

function Shell({ children }: { children: ReactNode }) {
  return <div data-testid="settings-root" className="mx-auto min-h-screen max-w-2xl bg-bg p-6 text-fg">{children}</div>;
}

function Row({ label, htmlFor, error, children }: { label: string; htmlFor: string; error?: string; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1 py-1.5">
      <div className="flex items-center justify-between gap-4">
        <label htmlFor={htmlFor} className="text-sm">
          {label}
        </label>
        <div className="w-48">{children}</div>
      </div>
      {error && (
        <p className="text-right text-xs text-bad" role="alert">
          {t(error)}
        </p>
      )}
    </div>
  );
}

function GeneralTab({ s, update }: TabProps) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col">
      <Row label={t("settings.language")} htmlFor="set-language">
        <SelectField
          id="set-language"
          label={t("settings.language")}
          hideLabel
          value={s.ui.language}
          options={LANGUAGES.map((l) => ({ value: l.code, label: l.name }))}
          onChange={(language) => update((d) => ({ ...d, ui: { ...d.ui, language } }))}
        />
      </Row>
      <Row label={t("settings.theme")} htmlFor="set-theme">
        <SelectField<Theme>
          id="set-theme"
          label={t("settings.theme")}
          hideLabel
          value={s.ui.theme}
          options={(["system", "dark", "light"] as const).map((v) => ({ value: v, label: t(`theme.${v}`) }))}
          onChange={(theme) => update((d) => ({ ...d, ui: { ...d.ui, theme } }))}
        />
      </Row>
      <SwitchField
        label={t("settings.startMinimised")}
        checked={s.ui.start_minimised}
        onChange={(start_minimised) => update((d) => ({ ...d, ui: { ...d.ui, start_minimised } }))}
      />
      <SwitchField
        label={t("settings.checkUpdates")}
        description={t("settings.checkUpdatesHelp")}
        checked={s.ui.check_updates}
        onChange={(check_updates) => update((d) => ({ ...d, ui: { ...d.ui, check_updates } }))}
      />
    </div>
  );
}

function HotkeysTab({ s, update, errors }: TabProps) {
  const { t } = useTranslation();
  // rebind_hotkey validates, persists and emits settings-changed, so apply it to base too.
  const persisted = useSettingsStore((st) => st.persisted);
  return (
    <div className="flex flex-col">
      <p className="mb-2 text-xs text-muted">{t("hotkeys.help")}</p>
      {HOTKEY_ACTIONS.map((action: HotkeyAction) => (
        <HotkeyRecorder
          key={action}
          action={action}
          chord={s.hotkeys[action]}
          others={HOTKEY_ACTIONS.filter((a) => a !== action).map((a) => s.hotkeys[a])}
          error={errors[`hotkeys.${action}`]}
          onBound={(chord) => persisted((d) => ({ ...d, hotkeys: { ...d.hotkeys, [action]: chord } }))}
        />
      ))}
      <SwitchField
        label={t("hotkeys.advancedCopy")}
        description={t("hotkeys.advancedCopyHelp")}
        checked={s.hotkeys.advanced_copy}
        onChange={(advanced_copy) => update((d) => ({ ...d, hotkeys: { ...d.hotkeys, advanced_copy } }))}
      />
    </div>
  );
}

function TradeTab({ s, update, errors }: TabProps) {
  const { t } = useTranslation();
  const leagues = useLeagues();
  const leagueOptions = (leagues.data ?? []).map((l) => ({ value: l.id, label: l.text ?? l.id }));
  const current = s.league.league;
  if (current && !leagueOptions.some((o) => o.value === current)) leagueOptions.unshift({ value: current, label: current });
  const setTrade = (patch: Partial<Settings_Serialize["trade"]>) => update((d) => ({ ...d, trade: { ...d.trade, ...patch } }));

  return (
    <div className="flex flex-col">
      <Row label={t("settings.source")} htmlFor="set-source">
        <SelectField<PriceSourceKind>
          id="set-source"
          label={t("settings.source")}
          hideLabel
          value={s.trade.source}
          options={(["trade", "poe_ninja", "poe2_scout"] as const).map((v) => ({ value: v, label: t(`source.${v}`) }))}
          onChange={(source) => setTrade({ source })}
        />
      </Row>
      <SwitchField
        label={t("settings.leagueAuto")}
        checked={s.league.auto_detect}
        onChange={(auto_detect) => update((d) => ({ ...d, league: { ...d.league, auto_detect } }))}
      />
      <Row label={t("settings.league")} htmlFor="set-league" error={errors["league.league"]}>
        {leagues.isError ? (
          <p className="text-xs text-bad" role="alert">
            {t("settings.leaguesFailed", { message: leagues.error.message })}
          </p>
        ) : (
          <SelectField
          id="set-league"
            label={t("settings.league")}
            hideLabel
            disabled={s.league.auto_detect || leagueOptions.length === 0}
            value={current ?? ""}
            options={leagueOptions}
            onChange={(league) => update((d) => ({ ...d, league: { ...d.league, league } }))}
          />
        )}
      </Row>
      <SwitchField label={t("settings.onlineOnly")} checked={s.trade.online_only} onChange={(online_only) => setTrade({ online_only })} />
      <Row label={t("settings.widenPct")} htmlFor="set-widen" error={errors["trade.widen_pct"]}>
        <NumberInput
          id="set-widen"
          label={t("settings.widenPct")}
          value={s.trade.widen_pct}
          step={1}
          invalid={Boolean(errors["trade.widen_pct"])}
          onChange={(widen_pct) => widen_pct !== null && setTrade({ widen_pct })}
        />
      </Row>
      <SwitchField
        label={t("settings.enableAllStats")}
        checked={s.trade.enable_all_stats}
        onChange={(enable_all_stats) => setTrade({ enable_all_stats })}
      />
      <Row label={t("settings.contact")} htmlFor="contact" error={errors["trade.contact"]}>
        <input
          id="contact"
          type="email"
          aria-invalid={Boolean(errors["trade.contact"]) || undefined}
          placeholder="you@example.com"
          value={s.trade.contact ?? ""}
          onChange={(e) => setTrade({ contact: e.target.value === "" ? null : e.target.value })}
          className={cx(
            "w-full rounded-md border bg-surface-2 px-2 py-1 text-sm outline-none focus-visible:ring-2 focus-visible:ring-gold",
            errors["trade.contact"] ? "border-bad" : "border-line",
          )}
        />
      </Row>
      <p className="text-xs text-muted">{t("settings.contactHelp")}</p>
    </div>
  );
}

function OverlayTab({ s, update, errors }: TabProps) {
  const { t } = useTranslation();
  const setOverlay = (patch: Partial<Settings_Serialize["overlay"]>) =>
    update((d) => ({ ...d, overlay: { ...d.overlay, ...patch } }));
  return (
    <div className="flex flex-col">
      <Row label={t("settings.autoHide")} htmlFor="set-autohide" error={errors["overlay.auto_hide_ms"]}>
        <NumberInput
          id="set-autohide"
          label={t("settings.autoHide")}
          value={s.overlay.auto_hide_ms}
          step={100}
          invalid={Boolean(errors["overlay.auto_hide_ms"])}
          onChange={(auto_hide_ms) => auto_hide_ms !== null && setOverlay({ auto_hide_ms })}
        />
      </Row>
      <Row label={t("settings.opacity")} htmlFor="set-opacity" error={errors["overlay.opacity"]}>
        <NumberInput
          id="set-opacity"
          label={t("settings.opacity")}
          value={s.overlay.opacity}
          step={0.05}
          invalid={Boolean(errors["overlay.opacity"])}
          onChange={(opacity) => opacity !== null && setOverlay({ opacity })}
        />
      </Row>
      <Row label={t("settings.side")} htmlFor="set-side">
        <SelectField<OverlaySide>
          id="set-side"
          label={t("settings.side")}
          hideLabel
          value={s.overlay.side}
          options={(["cursor", "left", "right"] as const).map((v) => ({ value: v, label: t(`side.${v}`) }))}
          onChange={(side) => setOverlay({ side })}
        />
      </Row>
    </div>
  );
}

type BackendChoice = BackendKind | "auto";

function PlatformTab({ s, update }: TabProps) {
  const { t } = useTranslation();
  const choices: BackendChoice[] = ["auto", "windows", "macos", "x11", "wayland", "fake"];
  return (
    <div className="flex flex-col">
      <Row label={t("settings.backend")} htmlFor="set-backend">
        <SelectField<BackendChoice>
          id="set-backend"
          label={t("settings.backend")}
          hideLabel
          value={s.platform.backend_override ?? "auto"}
          options={choices.map((v) => ({ value: v, label: t(`backend.${v}`) }))}
          onChange={(v) =>
            update((d) => ({ ...d, platform: { ...d.platform, backend_override: v === "auto" ? null : v } }))
          }
        />
      </Row>
      <p className="text-xs text-muted">{t("settings.backendHelp")}</p>
    </div>
  );
}
