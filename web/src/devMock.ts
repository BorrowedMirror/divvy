import { emit } from "@tauri-apps/api/event";
import { mockIPC } from "@tauri-apps/api/mocks";
import { diagnostics, itemCopied, leagues, priceSummary, settings } from "./test/fixtures";

/**
 * Browser-only preview (`pnpm dev` outside Tauri): fake the backend with fixtures so the views
 * can be designed and checked without the Rust side. Never loaded inside the Tauri webview.
 */
export function installDevMock(route: string): void {
  let current = settings();
  mockIPC(
    (cmd, args) => {
      const a = args as Record<string, unknown> | undefined;
      switch (cmd) {
        case "get_settings":
          return current;
        case "save_settings":
          current = a?.settings as typeof current;
          return current;
        case "get_diagnostics":
          return diagnostics();
        case "list_leagues":
          return leagues;
        case "get_permissions":
        case "recheck_permissions":
          return {
            platform: "macos",
            missing: [
              { kind: "accessibility", granted: false, required: true, settings_url: null },
              { kind: "screen_recording", granted: true, required: true, settings_url: null },
              { kind: "input_monitoring", granted: false, required: false, settings_url: null },
            ],
            notes: [],
            gnome: false,
          };
        case "query_price":
          return new Promise((r) => setTimeout(() => r(priceSummary()), 400));
        default:
          return null;
      }
    },
    { shouldMockEvents: true },
  );
  if (route === "overlay") setTimeout(() => void emit("item-copied", itemCopied()), 300);
}
