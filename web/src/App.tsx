import type { ReactNode } from "react";
import { useEffect, useState } from "react";

import { Toast } from "@base-ui/react/toast";
import { Tooltip } from "@base-ui/react/tooltip";
import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { BrandMark } from "@components/BrandMark";
import { createQueryClient, keys, useSettingsQuery } from "@hooks/queries";
import { useBackendEvent } from "@hooks/useBackendEvent";
import i18n, { supportedLanguage } from "@i18n/index";
import { commands, events } from "@src/bindings";
import { type Route, routeFromHash } from "@src/route";
import { DiagnosticsView } from "@views/diagnostics/DiagnosticsView";
import { NoticeView } from "@views/notice/NoticeView";
import { OnboardingView } from "@views/onboarding/OnboardingView";
import { OverlayView } from "@views/overlay/OverlayView";
import { SettingsView } from "@views/settings/SettingsView";
import { UpdateBanner, UpdateSync } from "@views/update/UpdateBanner";

/** Keeps the settings query in sync with `settings-changed` and applies language + theme. */
function SettingsSync() {
  const queryClient = useQueryClient();
  const settings = useSettingsQuery().data;
  useBackendEvent(events.settingsChanged, (p) => {
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

/** True while the backend shows the settings window as an overlay covering the game window. */
export function isGameOverlay(): boolean {
  return document.documentElement.dataset.overlay === "1";
}

/**
 * Settings-window chrome; the window is always borderless. In overlay mode
 * (`html[data-overlay="1"]`, set by the backend when the window covers the game) the page is a
 * transparent, dimmed backdrop with the content in a top-anchored card that sizes to the
 * current tab; Esc or a click on the backdrop closes it. Otherwise (`data-overlay="0"`) the
 * card fills the window, with a title bar to drag it by and the close button.
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
    // biome-ignore lint/a11y/noStaticElementInteractions: backdrop click-to-close; Esc is the keyboard path.
    <div
      className="k-shell"
      data-testid="settings-shell"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && isGameOverlay()) void commands.closeSettings();
      }}
    >
      <div className="k-card" role="dialog" aria-label={t("settings.title")}>
        {/* Borderless window away from the game: drag it by this bar (Wails' --wails-draggable). */}
        <div className="k-titlebar" data-testid="settings-titlebar">
          <BrandMark size={18} />
          <span className="text-sm font-semibold text-gold">Divvy</span>
        </div>
        <button
          type="button"
          className="k-overlay-close"
          aria-label={t("overlay.close")}
          onClick={() => void commands.closeSettings()}
        >
          ×
        </button>
        <div className="k-card-body">{children}</div>
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
  if (route === "notice") {
    return (
      <main data-route={route}>
        <NoticeView />
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
              <button
                type="button"
                className="text-sm text-muted underline hover:text-fg"
                onClick={() => navigate("settings")}
              >
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

/** Root component: providers + hash route (`overlay | notice | settings | diagnostics | onboarding`). */
export function App({ client }: { client?: QueryClient }) {
  return (
    <AppProviders client={client}>
      <Routes />
    </AppProviders>
  );
}
