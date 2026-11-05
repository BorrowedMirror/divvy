import type { PartialMessages } from "./en";

/**
 * German skeleton. Untranslated keys fall back to English (`fallbackLng: "en"`); the longer
 * platform guidance texts are intentionally left for a native speaker.
 */
export const de: PartialMessages = {
  common: { loading: "Lädt…" },
  overlay: {
    title: "Preisprüfung",
    empty: "Fahre im Spiel über einen Gegenstand und drücke {{hotkey}}.",
    openSettings: "Einstellungen öffnen",
    pin: "Overlay anheften",
    unpin: "Overlay lösen",
    close: "Overlay schließen",
    league: "Liga",
    widen: "Spanne ±%",
    status: "Verkäufer",
    stats: "Werte",
    misc: "Eigenschaften",
    min: "min",
    max: "max",
    search: "Suchen",
    loading: "Suche…",
  },
  item: { corrupted: "Verderbt", unidentified: "Nicht identifiziert" },
  misc: { item_level: "Gegenstandsstufe", quality: "Qualität", sockets: "Fassungen", corrupted: "Verderbt", any: "Egal", yes: "Ja", no: "Nein" },
  status: { securable: "Sofort", online: "Online", any: "Alle" },
  results: {
    total_one: "{{count}} Angebot",
    total_other: "{{count}} Angebote",
    empty: "Keine Angebote gefunden.",
    min: "Min",
    median: "Median",
    max: "Max",
    price: "Preis",
    seller: "Verkäufer",
    age: "Gelistet",
    openTrade: "Im Handel öffnen",
  },
  errors: {
    rate_limited: "Von der Handelsseite gedrosselt",
    internal: "Etwas ist schiefgelaufen",
    retryIn_one: "Erneut in {{count}} Sekunde",
    retryIn_other: "Erneut in {{count}} Sekunden",
  },
  settings: {
    title: "Einstellungen",
    tabs: { general: "Allgemein", hotkeys: "Tastenkürzel", trade: "Handel", overlay: "Overlay", platform: "Plattform" },
    language: "Sprache",
    theme: "Design",
    league: "Liga",
    save: "Speichern",
    saved: "Einstellungen gespeichert",
    discard: "Änderungen verwerfen",
  },
  theme: { system: "System", dark: "Dunkel", light: "Hell" },
  diagnostics: { title: "Diagnose", copyReport: "Bericht kopieren", recentErrors: "Letzte Fehler" },
};
