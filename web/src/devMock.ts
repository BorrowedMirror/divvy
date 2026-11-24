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
/** The platform the preview pretends to run on, from `?platform=` in the hash (macOS by default). */
export type PreviewPlatform = "macos" | "linux" | "windows";

export function previewPlatform(hash: string): PreviewPlatform {
  const p = /[?&]platform=(linux|windows)\b/.exec(hash)?.[1];
  return p === "linux" || p === "windows" ? p : "macos";
}

// What the X11 backend reports on a Linux desktop with XWayland (internal/platform/x11): every
// capability, and the note it adds under XWayland. Paths follow the XDG directories.
const xwaylandNote = "Running through XWayland; the game must also run under XWayland (the default for Proton/Wine)";
const linux = {
  diagnostics: diagnostics({
    os: "linux",
    backend: "x11",
    capabilities: {
      window_tracking: true,
      overlay_always_on_top: true,
      overlay_click_through: true,
      global_hotkeys: true,
      passive_hook: true,
      keystroke_injection: true,
      clipboard: true,
      missing_permissions: [],
      notes: [xwaylandNote],
    },
    settings_path: "/home/exile/.config/divvy/settings.toml",
    log_path: "/home/exile/.local/state/divvy/logs/divvy.log",
    recent_errors: [],
  }),
  permissions: { platform: "x11" as const, missing: [], notes: [xwaylandNote], gnome: false },
};

// What the Windows backend reports (internal/platform/win): every capability, with Wails' hotkeys
// and clipboard, and its two notes. Paths follow %APPDATA% and %LOCALAPPDATA%.
const windowsNotes = [
  "If Path of Exile 2 runs as administrator, Divvy must too: Windows (UIPI) blocks injected Ctrl+C into elevated windows.",
  "Use windowed or borderless fullscreen; exclusive fullscreen hides the overlay.",
];
const windows = {
  diagnostics: diagnostics({
    os: "windows",
    backend: "windows",
    capabilities: {
      window_tracking: true,
      overlay_always_on_top: true,
      overlay_click_through: true,
      global_hotkeys: true,
      passive_hook: true,
      keystroke_injection: true,
      clipboard: true,
      missing_permissions: [],
      notes: windowsNotes,
    },
    settings_path: "C:\\Users\\exile\\AppData\\Roaming\\divvy\\config\\settings.toml",
    log_path: "C:\\Users\\exile\\AppData\\Local\\divvy\\data\\logs\\divvy.log",
    recent_errors: [],
  }),
  permissions: { platform: "windows" as const, missing: [], notes: windowsNotes, gnome: false },
};

const macos = {
  diagnostics: diagnostics(),
  permissions: {
    platform: "macos" as const,
    missing: [
      { kind: "accessibility", granted: false, required: true, settings_url: null },
      { kind: "screen_recording", granted: true, required: true, settings_url: null },
      { kind: "input_monitoring", granted: false, required: false, settings_url: null },
    ],
    notes: [],
    gnome: false,
  },
};

export function installDevMock(route: string, platform: PreviewPlatform = previewPlatform(window.location.hash)): void {
  const system = { linux, macos, windows }[platform];
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
        return system.diagnostics;
      case "list_leagues":
        return leagues;
      case "get_permissions":
      case "recheck_permissions":
        return system.permissions;
      case "query_price":
        return new Promise((r) => setTimeout(() => r(priceSummary()), 400));
      // The bundled data's entries (internal/data statics) for the currencies the fixtures price in.
      case "currency_icons":
        return [
          {
            id: "divine",
            name: "Divine Orb",
            icon: "https://web.poecdn.com/gen/image/WzI1LDE0LHsiZiI6IjJESXRlbXMvQ3VycmVuY3kvQ3VycmVuY3lNb2RWYWx1ZXMiLCJzY2FsZSI6MSwicmVhbG0iOiJwb2UyIn1d/2986e220b3/CurrencyModValues.png",
          },
          {
            id: "exalted",
            name: "Exalted Orb",
            icon: "https://web.poecdn.com/gen/image/WzI1LDE0LHsiZiI6IjJESXRlbXMvQ3VycmVuY3kvQ3VycmVuY3lBZGRNb2RUb1JhcmUiLCJzY2FsZSI6MSwicmVhbG0iOiJwb2UyIn1d/ad7c366789/CurrencyAddModToRare.png",
          },
          {
            id: "chaos",
            name: "Chaos Orb",
            icon: "https://web.poecdn.com/gen/image/WzI1LDE0LHsiZiI6IjJESXRlbXMvQ3VycmVuY3kvQ3VycmVuY3lSZXJvbGxSYXJlIiwic2NhbGUiOjEsInJlYWxtIjoicG9lMiJ9XQ/c0ca392a78/CurrencyRerollRare.png",
          },
        ];
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
