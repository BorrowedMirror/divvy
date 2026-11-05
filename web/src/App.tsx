import { Toast } from "@base-ui/react/toast";
import { Tooltip } from "@base-ui/react/tooltip";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { events } from "./bindings";
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

export function Routes() {
  const [route, navigate] = useRoute();
  const { t } = useTranslation();
  return (
    <main data-route={route} className="min-h-screen">
      {(route === "settings" || route === "diagnostics") && <UpdateBanner />}
      {route === "overlay" && <OverlayView />}
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
