import { QueryClient, useMutation, useQuery } from "@tanstack/react-query";
import { commands } from "../bindings";
import type { FilterSet_Serialize, PriceSummary_Serialize, Settings_Serialize } from "../bindings";
import { unwrap } from "../lib/ipc";

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
