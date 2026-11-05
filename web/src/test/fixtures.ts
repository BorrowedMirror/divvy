import type {
  Diagnostics_Serialize,
  FilterSet_Serialize,
  IpcError_Serialize,
  ItemCopied_Serialize,
  League_Serialize,
  ParsedItem_Serialize,
  PriceSummary_Serialize,
  Settings_Serialize,
} from "../bindings";

export function parsedItem(overrides: Partial<ParsedItem_Serialize> = {}): ParsedItem_Serialize {
  return {
    class: { type: "helmet" },
    category: "equipment",
    rarity: "rare",
    name: "Doom Visor",
    base_type: "Expert Hunter Hood",
    item_level: 82,
    quality: 20,
    stack_size: null,
    gem_level: null,
    waystone_tier: null,
    area_level: null,
    sockets: 1,
    requirements: { level: 65, str: null, dex: 100, int: null },
    stats: {
      armour: null,
      evasion: 412,
      energy_shield: null,
      spirit: null,
      block_chance: null,
      physical_dps: null,
      elemental_dps: null,
      total_dps: null,
      attacks_per_second: null,
      critical_hit_chance: null,
    },
    properties: [{ name: "Evasion Rating", value: "412", augmented: true }],
    modifiers: [
      {
        text: "+85 to maximum Life",
        template: "+# to maximum Life",
        kind: "explicit",
        values: [85],
        stat_id: "explicit.stat_3299347043",
        advanced: null,
      },
    ],
    corrupted: true,
    unidentified: false,
    mirrored: false,
    sanctified: false,
    note: null,
    advanced: false,
    raw: "Item Class: Helmets\nRarity: Rare\nDoom Visor\nExpert Hunter Hood",
    ...overrides,
  };
}

const range = (enabled = false, min: number | null = null, max: number | null = null) => ({ enabled, min, max });

export function filterSet(overrides: Partial<FilterSet_Serialize> = {}): FilterSet_Serialize {
  return {
    league: "Standard",
    status: "online",
    name: null,
    base_type: "Expert Hunter Hood",
    category: "armour.helmet",
    rarity: "nonunique",
    stats: [
      {
        stat_id: "explicit.stat_3299347043",
        text: "+85 to maximum Life",
        enabled: true,
        min: 76,
        max: null,
        item_values: [85],
      },
      {
        stat_id: "explicit.stat_4220027924",
        text: "+32% to Cold Resistance",
        enabled: false,
        min: 28,
        max: null,
        item_values: [32],
      },
    ],
    misc: {
      item_level: range(true, 82),
      quality: range(),
      gem_level: range(),
      waystone_tier: range(),
      sockets: range(),
      corrupted: true,
      identified: null,
    },
    widen_pct: 10,
    route: "search",
    ...overrides,
  };
}

export function itemCopied(overrides: Partial<ItemCopied_Serialize> = {}): ItemCopied_Serialize {
  return { item: parsedItem(), filters: filterSet(), pinned: false, ...overrides };
}

export function priceSummary(overrides: Partial<PriceSummary_Serialize> = {}): PriceSummary_Serialize {
  return {
    source: "trade",
    total: 57,
    listings: [
      {
        id: "a1",
        price: { amount: 2, currency: "divine" },
        seller: "ExileOne",
        online: true,
        indexed: "2026-11-05T11:50:00Z",
        item_level: 82,
        stack_size: null,
      },
      {
        id: "b2",
        price: { amount: 3.5, currency: "divine" },
        seller: null,
        online: false,
        indexed: "2026-11-04T09:00:00Z",
        item_level: 83,
        stack_size: null,
      },
    ],
    min: { amount: 2, currency: "divine" },
    median: { amount: 2.75, currency: "divine" },
    max: { amount: 3.5, currency: "divine" },
    trade_url: "https://www.pathofexile.com/trade2/search/poe2/Standard/abc",
    ...overrides,
  };
}

export function settings(overrides: Partial<Settings_Serialize> = {}): Settings_Serialize {
  return {
    version: 1,
    league: { auto_detect: true, league: "Standard" },
    hotkeys: {
      price_check: { modifiers: ["ctrl"], key: "KeyD" },
      price_check_pinned: { modifiers: ["ctrl", "alt"], key: "KeyD" },
      open_settings: { modifiers: [], key: "F10" },
      advanced_copy: false,
    },
    trade: {
      source: "trade",
      online_only: true,
      widen_pct: 10,
      enable_all_stats: false,
      contact: null,
      poesessid: null,
    },
    overlay: { auto_hide_ms: 1500, opacity: 1, side: "cursor" },
    ui: { language: "en", theme: "dark", start_minimised: false, check_updates: true, onboarding_done: false },
    platform: { backend_override: null },
    ...overrides,
  };
}

export function diagnostics(overrides: Partial<Diagnostics_Serialize> = {}): Diagnostics_Serialize {
  return {
    version: "0.1.0-rc.1",
    os: "macos",
    backend: "macos",
    capabilities: {
      window_tracking: true,
      overlay_always_on_top: true,
      overlay_click_through: false,
      global_hotkeys: true,
      passive_hook: false,
      keystroke_injection: false,
      clipboard: true,
      missing_permissions: ["accessibility", "screen_recording"],
      notes: ["Overlay uses NSPanel"],
    },
    data_version: "0.3.1",
    league: "Standard",
    settings_path: "/Users/exile/Library/Application Support/kalandra/settings.toml",
    log_path: null,
    recent_errors: ["trade: 503 Service Unavailable"],
    ...overrides,
  };
}

export const leagues: League_Serialize[] = [
  { id: "Standard", realm: "poe2", text: "Standard" },
  { id: "Dawn of the Hunt", realm: "poe2", text: "Dawn of the Hunt" },
];

export function ipcError(kind: string, message = "boom", retry_after_ms: number | null = null): IpcError_Serialize {
  return { kind, message, retry_after_ms };
}
