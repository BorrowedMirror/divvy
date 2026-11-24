import { mockTransport } from "@lib/mockTransport";
import { setTransport } from "@src/bindings";
import { diagnostics, itemCopied, leagues, priceSummary, settings } from "@test/fixtures";

declare global {
  interface Window {
    /** The preview's backend, for scripts driving it (`make screenshots`): `emit` sends an event. */
    divvyPreview?: { emit: (event: string, payload: unknown) => Promise<void> };
  }
}

/**
 * Browser-only preview (`pnpm dev` outside the app): fake the backend with fixtures so the
 * views can be designed and checked without the Go side. Never loaded inside the app's web view.
 */
export function installDevMock(route: string): void {
  let current = settings();
  // The fixtures rebind this for the tests; show the default the app ships with.
  current.hotkeys.open_settings = { modifiers: ["ctrl"], key: "Space" };
  const { transport, emit } = mockTransport((cmd, args) => {
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
      // A sample of the real catalogue (internal/app's waystoneCatalog over the bundled data), short
      // enough that the list doesn't scroll.
      case "waystone_mods":
        return [
          { text: "Players are Cursed with Enfeeble", group: "player" },
          { text: "Players are Cursed with Temporal Chains", group: "player" },
          { text: "Players have -#% to all maximum Resistances", group: "player" },
          { text: "Map Bosses grant #% increased Experience", group: "monster" },
          { text: "Map Bosses have # additional Modifier", group: "monster" },
          { text: "#% increased Pack Size in Map", group: "area" },
          { text: "#% increased Gold found in this Area", group: "area" },
        ];
      // A plain window: the card where the backend would show it, not over a covering backdrop.
      case "get_overlay_layout":
        return { cover: false, x: 0, y: 0 };
      // The overlay catches up from this once it listens, so a slow first load still shows the item.
      case "get_overlay_snapshot":
        return route === "overlay" ? itemCopied() : null;
      default:
        return null;
    }
  });
  setTransport(transport);
  window.divvyPreview = { emit };
  if (route === "overlay") setTimeout(() => void emit("item-copied", itemCopied()), 300);
}
