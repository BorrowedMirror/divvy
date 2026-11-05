import { Toast } from "@base-ui/react/toast";
import { Tooltip } from "@base-ui/react/tooltip";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { commands, events } from "./bindings";
import { createQueryClient, keys, useSettingsQuery } from "./hooks/queries";
import { useTauriEvent } from "./hooks/useTauriEvent";
import i18n, { supportedLanguage } from "./i18n";
import { type Route, routeFromHash } from "./route";
import { DiagnosticsView } from "./views/diagnostics/DiagnosticsView";
import { OnboardingView } from "./views/onboarding/OnboardingView";
import { OverlayView } from "./views/overlay/OverlayView";
import { SettingsView } from "./views/settings/SettingsView";
import { UpdateBanner, UpdateSync } from "./views/update/UpdateBanner";

/** Keeps the settings query in sync with `settings-changed` and applies language + theme. */
function SettingsSync() {
  const queryClient = useQueryClient();
  const settings = useSettingsQuery().data;
  useTauriEvent(events.settingsChanged, (p) => {
    queryClient.setQueryData(keys.settings, p.settings);
  });
  const language = supportedLanguage(settings?.ui.language);
  const theme = settings?.ui.theme ?? "system";
  useEffect(() => {
    if (i18n.language !== language) void i18n.changeLanguage(language);
    document.documentElement.lang = language;
  }, [language]);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);
  return null;
}

function Toasts() {
  const { toasts } = Toast.useToastManager();
  const { t } = useTranslation();
  return (
    <Toast.Portal>
      <Toast.Viewport className="fixed right-4 bottom-4 z-50 flex w-72 flex-col gap-2">
        {toasts.map((toast) => (
          <Toast.Root
            key={toast.id}
            toast={toast}
            className="rounded-md border border-gold/40 bg-surface-2 px-3 py-2 text-sm text-fg shadow-lg"
          >
            <Toast.Content className="flex items-center justify-between gap-2">
              <div className="flex min-w-0 flex-col">
                <Toast.Title />
                <Toast.Description className="text-xs text-muted" />
              </div>
              <Toast.Close aria-label={t("common.dismiss")} className="text-muted hover:text-fg">
                ×
              </Toast.Close>
            </Toast.Content>
          </Toast.Root>
        ))}
      </Toast.Viewport>
    </Toast.Portal>
  );
}

export function AppProviders({ children, client }: { children: ReactNode; client?: QueryClient }) {
  const [queryClient] = useState(() => client ?? createQueryClient());
  return (
    <QueryClientProvider client={queryClient}>
      <Toast.Provider>
        <Tooltip.Provider delay={400}>
          <SettingsSync />
          <UpdateSync />
          {children}
          <Toasts />
        </Tooltip.Provider>
      </Toast.Provider>
    </QueryClientProvider>
  );
}

function useRoute(): [Route, (r: Route) => void] {
  const [route, setRoute] = useState(() => routeFromHash(window.location.hash));
  useEffect(() => {
    const onHash = () => setRoute(routeFromHash(window.location.hash));
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  useEffect(() => {
    document.documentElement.dataset.route = route;
  }, [route]);
  const navigate = (r: Route) => {
    window.location.hash = `#/${r}`;
    setRoute(r);
  };
  return [route, navigate];
}

/** True while Rust shows the settings window as an overlay covering the game window. */
export function isGameOverlay(): boolean {
  return document.documentElement.dataset.overlay === "1";
}

/**
 * Settings-window chrome. In overlay mode (`html[data-overlay="1"]`, set by Rust when the
 * window covers the game) the page is a transparent, dimmed backdrop with the content in a
 * centred card; Esc or a click on the backdrop closes it. Otherwise it is a plain page.
 */
function SettingsShell({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Popups, selects and the hotkey recorder handle (and prevent) their own Escape.
      if (e.key !== "Escape" || e.defaultPrevented || !isGameOverlay()) return;
      void commands.closeSettings();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <div
      className="k-shell"
      data-testid="settings-shell"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && isGameOverlay()) void commands.closeSettings();
      }}
    >
      <div className="k-card" role="dialog" aria-label={t("settings.title")}>
        <button
          type="button"
          className="k-overlay-close"
          aria-label={t("overlay.close")}
          onClick={() => void commands.closeSettings()}
        >
          ×
        </button>
        {children}
      </div>
    </div>
  );
}

export function Routes() {
  const [route, navigate] = useRoute();
  const { t } = useTranslation();
  if (route === "overlay") {
    return (
      <main data-route={route} className="min-h-screen">
        <OverlayView />
      </main>
    );
  }
  return (
    <main data-route={route} className="min-h-screen">
      <SettingsShell>
        {(route === "settings" || route === "diagnostics") && <UpdateBanner />}
        {route === "settings" && <SettingsView onOpenDiagnostics={() => navigate("diagnostics")} />}
        {route === "onboarding" && <OnboardingView onDone={() => navigate("settings")} />}
        {route === "diagnostics" && (
          <>
            <nav className="mx-auto max-w-2xl px-6 pt-4">
              <button type="button" className="text-sm text-muted underline hover:text-fg" onClick={() => navigate("settings")}>
                ← {t("settings.title")}
              </button>
            </nav>
            <DiagnosticsView />
          </>
        )}
      </SettingsShell>
    </main>
  );
}

/** Root component: providers + hash route (`overlay | settings | diagnostics | onboarding`). */
export function App({ client }: { client?: QueryClient }) {
  return (
    <AppProviders client={client}>
      <Routes />
    </AppProviders>
  );
}
