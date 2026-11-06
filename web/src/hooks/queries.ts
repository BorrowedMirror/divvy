import { QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useTauriEvent } from "@hooks/useTauriEvent";
import { unwrap } from "@lib/ipc";
import type { FilterSet_Serialize, PriceSummary_Serialize, Settings_Serialize } from "@src/bindings";
import { commands, events } from "@src/bindings";

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, staleTime: 60_000 },
      mutations: { retry: false },
    },
  });
}

export const keys = {
  settings: ["settings"] as const,
  leagues: ["leagues"] as const,
  diagnostics: ["diagnostics"] as const,
  permissions: ["permissions"] as const,
};

export function useSettingsQuery() {
  return useQuery({ queryKey: keys.settings, queryFn: () => commands.getSettings() });
}

export function useLeagues() {
  return useQuery({
    queryKey: keys.leagues,
    queryFn: async () => unwrap(await commands.listLeagues()),
    staleTime: 10 * 60_000,
  });
}

export function useDiagnostics() {
  return useQuery({ queryKey: keys.diagnostics, queryFn: () => commands.getDiagnostics() });
}

export function useQueryPrice() {
  return useMutation<PriceSummary_Serialize, Error, FilterSet_Serialize>({
    mutationFn: async (filters) => unwrap(await commands.queryPrice(filters)),
  });
}

export function useSaveSettings() {
  return useMutation<Settings_Serialize, Error, Settings_Serialize>({
    mutationFn: async (settings) => unwrap(await commands.saveSettings(settings)),
  });
}

/** How often the onboarding view re-checks OS permissions while it is visible. */
export const PERMISSION_POLL_MS = 2000;

/** Poll only while the document is visible; a hidden settings window does no work. */
export function permissionPollInterval(): number | false {
  return document.visibilityState === "visible" ? PERMISSION_POLL_MS : false;
}

/**
 * Live permission report. While mounted it re-checks every 2 s (paused when the window is
 * hidden) and also applies `permissions-changed` pushes from the backend.
 */
export function usePermissions() {
  const queryClient = useQueryClient();
  useTauriEvent(events.permissionsChanged, (p) => {
    queryClient.setQueryData(keys.permissions, p.report);
  });
  return useQuery({
    queryKey: keys.permissions,
    queryFn: () => commands.recheckPermissions(),
    refetchInterval: permissionPollInterval,
    refetchIntervalInBackground: false,
    staleTime: 0,
  });
}

export function useRequestPermission() {
  return useMutation<null, Error, string>({
    mutationFn: async (kind) => unwrap(await commands.requestPermission(kind)),
  });
}
