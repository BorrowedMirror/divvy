import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test } from "vitest";

import type { Settings_Serialize } from "@src/bindings";
import { useSettingsStore } from "@stores/settings";
import { ipcError, settings } from "@test/fixtures";
import { callsOf, emitEvent, fail, flush, renderWithProviders, setupIpc } from "@test/ipc";
import { SettingsView } from "@views/settings/SettingsView";

async function mount(handlers: Parameters<typeof setupIpc>[0] = {}) {
  const calls = setupIpc(handlers);
  const utils = renderWithProviders(<SettingsView onOpenDiagnostics={() => {}} />);
  await screen.findByRole("tab", { name: "General" });
  return { calls, ...utils };
}

const draft = () => useSettingsStore.getState().draft as Settings_Serialize;

describe("SettingsView", () => {
  test("shows loading then the general tab", async () => {
    await mount();
    expect(screen.getByRole("heading", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Language" })).toHaveTextContent("English");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  test("load failure shows an error", async () => {
    setupIpc({ get_settings: () => fail("nope") });
    renderWithProviders(<SettingsView />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load settings.");
  });

  test("general tab edits enable save; save calls save_settings and toasts", async () => {
    const user = userEvent.setup();
    const { calls } = await mount();
    await user.click(screen.getByRole("combobox", { name: "Theme" }));
    await user.click(await screen.findByRole("option", { name: "Light" }));
    await user.click(screen.getByRole("switch", { name: "Start minimised to tray" }));
    expect(draft().ui).toEqual({
      language: "en",
      theme: "light",
      start_minimised: true,
      check_updates: true,
      onboarding_done: false,
      autostart: false,
    });

    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(callsOf(calls, "save_settings")).toHaveLength(1));
    expect((callsOf(calls, "save_settings")[0]?.args?.settings as Settings_Serialize | undefined)?.ui.theme).toBe(
      "light",
    );
    expect(await screen.findByText("Settings saved")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  test("check for updates switch toggles ui.check_updates", async () => {
    const user = userEvent.setup();
    await mount();
    const toggle = screen.getByRole("switch", { name: /Check for updates automatically/ });
    expect(toggle).toBeChecked();
    await user.click(toggle);
    expect(draft().ui.check_updates).toBe(false);
  });

  test("discard reverts edits", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("switch", { name: "Start minimised to tray" }));
    await user.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(draft().ui.start_minimised).toBe(false);
  });

  test("save errors are displayed", async () => {
    const user = userEvent.setup();
    await mount({ save_settings: () => fail(ipcError("config", "disk full")) });
    await user.click(screen.getByRole("switch", { name: "Start minimised to tray" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Couldn't save: disk full")).toBeInTheDocument();
  });

  test("trade tab: source, league, toggles, widen, contact validation", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Trade" }));

    await user.click(screen.getByRole("combobox", { name: "Price source" }));
    await user.click(await screen.findByRole("option", { name: "poe2scout" }));
    expect(draft().trade.source).toBe("poe2_scout");

    const league = screen.getByRole("combobox", { name: "League" });
    expect(league).toHaveAttribute("data-disabled");
    await user.click(screen.getByRole("switch", { name: "Detect league automatically" }));
    await user.click(screen.getByRole("combobox", { name: "League" }));
    await user.click(await screen.findByRole("option", { name: "Dawn of the Hunt" }));
    expect(draft().league).toEqual({ auto_detect: false, league: "Dawn of the Hunt" });

    expect(screen.getByRole("combobox", { name: "Default sellers" })).toHaveTextContent("Any");
    await user.click(screen.getByRole("combobox", { name: "Default sellers" }));
    await user.click(await screen.findByRole("option", { name: "Online" }));
    await user.click(screen.getByRole("switch", { name: "Enable all stats by default" }));
    expect(draft().trade).toMatchObject({ sellers: "online", enable_all_stats: true });

    const widen = screen.getByRole("textbox", { name: "Widen stat ranges (%)" });
    fireEvent.change(widen, { target: { value: "150" } });
    fireEvent.blur(widen);
    expect(await screen.findByText("Enter a value between 0 and 100.")).toBeInTheDocument();

    const contact = screen.getByLabelText("Contact e-mail");
    await user.type(contact, "nope");
    expect(screen.getByText("Enter a valid e-mail address or leave it empty.")).toBeInTheDocument();
    expect(screen.getByText("Fix the highlighted fields before saving.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await user.clear(contact);
    expect(draft().trade.contact).toBeNull();
  });

  test("trade tab shows league load failures", async () => {
    const user = userEvent.setup();
    await mount({ list_leagues: () => fail(ipcError("trade", "offline")) });
    await user.click(screen.getByRole("tab", { name: "Trade" }));
    expect(await screen.findByText("Couldn't load leagues: offline")).toBeInTheDocument();
  });

  test("overlay tab edits auto-hide, opacity and side", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    const autoHide = screen.getByRole("textbox", { name: "Auto-hide delay (ms)" });
    fireEvent.change(autoHide, { target: { value: "3000" } });
    fireEvent.blur(autoHide);
    const opacity = screen.getByRole("textbox", { name: "Opacity (0.1–1)" });
    fireEvent.change(opacity, { target: { value: "0.05" } });
    fireEvent.blur(opacity);
    expect(await screen.findByText("Enter a value between 0.1 and 1.")).toBeInTheDocument();
    fireEvent.change(autoHide, { target: { value: "10001" } });
    fireEvent.blur(autoHide);
    expect(await screen.findByText(/between 0 and 10000/)).toBeInTheDocument();
    await user.click(screen.getByRole("combobox", { name: "Position" }));
    await user.click(await screen.findByRole("option", { name: "Right edge" }));
    expect(draft().overlay).toMatchObject({ auto_hide_ms: 10001, opacity: 0.05, side: "right" });
  });

  test("overlay tab sets seller grouping and listing columns, trade tab the listing count", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    await user.click(screen.getByRole("switch", { name: "Group listings by seller" }));
    await user.click(screen.getByRole("checkbox", { name: "Item level" }));
    await user.click(screen.getByRole("checkbox", { name: "Seller" }));
    expect(draft().overlay).toMatchObject({ group_by_seller: false, listing_columns: ["item_level", "listed"] });
    await user.click(screen.getByRole("tab", { name: "Trade" }));
    await user.click(screen.getByRole("combobox", { name: "Listings to fetch" }));
    await user.click(await screen.findByRole("option", { name: "40" }));
    await user.click(screen.getByRole("switch", { name: /Compare prices across currencies/ }));
    expect(draft().trade).toMatchObject({ listing_count: 40, convert_prices: false });
    await user.click(screen.getByRole("switch", { name: /Remember stat choices per item type/ }));
    expect(draft().trade.remember_filters).toBe(false);
  });

  test("batch 3 settings: panel size, backdrop, close on game focus, start on login", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    await user.click(screen.getByRole("combobox", { name: "Panel size" }));
    await user.click(await screen.findByRole("option", { name: "125%" }));
    await user.click(screen.getByRole("combobox", { name: "Backdrop behind the panel" }));
    await user.click(await screen.findByRole("option", { name: "Strong" }));
    await user.click(screen.getByRole("switch", { name: "Close a pinned panel when I click back into the game" }));
    expect(draft().overlay).toMatchObject({ scale: 1.25, backdrop_dim: 0.8, close_pinned_on_game_focus: true });
    await user.click(screen.getByRole("tab", { name: "General" }));
    await user.click(screen.getByRole("switch", { name: "Start RKL when I log in" }));
    expect(draft().ui.autostart).toBe(true);
  });

  test("a ?tab= hash opens that tab, also while already open", async () => {
    await mount({ waystone_mods: () => [] });
    expect(screen.getByRole("tab", { name: "General" })).toHaveAttribute("aria-selected", "true");
    await act(async () => {
      window.location.hash = "#/settings?tab=waystones";
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(screen.getByRole("tab", { name: "Waystones" })).toHaveAttribute("aria-selected", "true");
    window.location.hash = "";
  });

  test("waystones tab flags mods from the catalogue, searches and applies presets", async () => {
    const user = userEvent.setup();
    await mount({
      waystone_mods: () => [
        { text: "Players have -#% to all maximum Resistances", group: "player" },
        { text: "Monsters fire # additional Projectiles", group: "monster" },
        { text: "#% increased Pack Size in Map", group: "area" },
      ],
    });
    await user.click(screen.getByRole("tab", { name: "Waystones" }));
    expect(await screen.findAllByTestId("waystone-catalog-row")).toHaveLength(3);
    const projectiles = screen.getByRole("radiogroup", { name: "Flag for Monsters fire # additional Projectiles" });
    expect(within(projectiles).getByRole("radio", { name: "Ignore" })).toHaveAttribute("aria-checked", "true");
    const res = screen.getByRole("radiogroup", { name: "Flag for Players have -#% to all maximum Resistances" });
    expect(within(res).getByRole("radio", { name: "Danger" })).toHaveAttribute("aria-checked", "true");
    await user.click(within(projectiles).getByRole("radio", { name: "Warn" }));
    expect(draft().waystone.flags["Monsters fire # additional Projectiles"]).toBe("warn");
    await user.click(
      within(screen.getByRole("radiogroup", { name: "Flag for #% increased Pack Size in Map" })).getByRole("radio", {
        name: "Ignore",
      }),
    );
    expect(draft().waystone.flags["#% increased Pack Size in Map"]).toBeUndefined();
    await user.type(screen.getByRole("searchbox", { name: "Search waystone mods" }), "pack");
    await waitFor(() => expect(screen.getAllByTestId("waystone-catalog-row")).toHaveLength(1));
    await user.click(screen.getByRole("combobox", { name: "Preset" }));
    await user.click(await screen.findByRole("option", { name: "Clear all flags" }));
    expect(draft().waystone.flags).toEqual({});
  });

  test("item link tickboxes", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    await user.click(screen.getByRole("checkbox", { name: "Wiki" }));
    expect(draft().overlay.links).toEqual({ poe2db: true, wiki: false, craft_of_exile: true });
  });

  test("in game tab edits chat commands, stash searches and ctrl+scroll", async () => {
    const user = userEvent.setup();
    const { calls } = await mount();
    await user.click(screen.getByRole("tab", { name: "In game" }));
    expect(screen.getAllByTestId("chat-command")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Add command" }));
    const text = screen.getByRole("textbox", { name: "Command 2" });
    await user.clear(text);
    await user.type(text, "/kingsmarch");
    await user.click(
      within(screen.getAllByTestId("chat-command")[1] as HTMLElement).getByRole("checkbox", { name: "Send" }),
    );
    const hotkey = screen.getByRole("button", { name: /^Command 2 hotkey/ });
    await user.click(hotkey);
    fireEvent.keyDown(hotkey, { code: "F6" });
    expect(draft().chat.commands[1]).toEqual({ chord: { modifiers: [], key: "F6" }, text: "/kingsmarch", send: false });
    expect(callsOf(calls, "rebind_hotkey")).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Add search" }));
    await user.type(screen.getByRole("textbox", { name: "Search 1" }), "rarity: rare");
    expect(draft().stash.searches[0]?.text).toBe("rarity: rare");
    await user.click(screen.getByRole("switch", { name: /Switch stash tabs with Ctrl/ }));
    expect(draft().stash.ctrl_scroll_tabs).toBe(true);
    await user.click(screen.getByRole("button", { name: "Remove /kingsmarch" }));
    expect(draft().chat.commands).toHaveLength(1);
  });

  test("in game tab explains placeholders and finds the game log", async () => {
    const user = userEvent.setup();
    const found = "/games/Path of Exile 2/logs/Client.txt";
    const { calls } = await mount({ find_game_log: (a) => (a?.configured === "" ? found : null) });
    await user.click(screen.getByRole("tab", { name: "In game" }));
    expect(screen.getByText("Your current area.")).toBeInTheDocument();
    expect(screen.getByText(/@last ty&&\/kick @last/)).toBeInTheDocument();
    expect(await screen.findByText(`Reading ${found}`)).toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Game log file (Client.txt)" }), "/nope");
    expect(await screen.findByText(/No game log found/)).toBeInTheDocument();
    expect(draft().chat).toEqual({ commands: draft().chat.commands, game_log: "/nope" });
    expect(draft().chat.commands).toHaveLength(1);
    expect(callsOf(calls, "find_game_log").at(-1)?.args).toEqual({ configured: "/nope" });
  });

  test("in game hotkeys reject a chord another hotkey uses", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "In game" }));
    await user.click(screen.getByRole("button", { name: "Add search" }));
    const hotkey = screen.getByRole("button", { name: /^Search 1 hotkey/ });
    await user.click(hotkey);
    fireEvent.keyDown(hotkey, { code: "F5" });
    expect(await screen.findByText("This shortcut is already used by another action.")).toBeInTheDocument();
    expect(draft().stash.searches[0]?.chord.key).toBe("");
  });

  test("forget remembered stat choices", async () => {
    const user = userEvent.setup();
    const { calls } = await mount({ clear_filter_memory: () => null });
    await user.click(screen.getByRole("tab", { name: "Trade" }));
    await user.click(screen.getByRole("button", { name: "Forget remembered choices" }));
    expect(callsOf(calls, "clear_filter_memory")).toHaveLength(1);
    expect(await screen.findByText("Forgotten.")).toBeInTheDocument();
  });

  test("clearing a numeric field keeps the last valid value instead of null", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    for (const name of ["Auto-hide delay (ms)", "Opacity (0.1–1)"]) {
      const input = screen.getByRole("textbox", { name });
      fireEvent.change(input, { target: { value: "" } });
      fireEvent.blur(input);
    }
    await user.click(screen.getByRole("tab", { name: "Trade" }));
    const widen = screen.getByRole("textbox", { name: "Widen stat ranges (%)" });
    fireEvent.change(widen, { target: { value: "" } });
    fireEvent.blur(widen);
    expect(draft().overlay).toMatchObject({ auto_hide_ms: 1500, opacity: 1 });
    expect(draft().trade.widen_pct).toBe(10);
  });

  test("labels point at their controls", async () => {
    await mount();
    expect(screen.getByText("Language", { selector: "label" })).toHaveAttribute("for", "set-language");
    expect(document.getElementById("set-language")).not.toBeNull();
  });

  test("platform tab sets and clears the backend override", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("tab", { name: "Platform" }));
    await user.click(screen.getByRole("combobox", { name: "Platform backend" }));
    await user.click(await screen.findByRole("option", { name: "X11 / XWayland" }));
    expect(draft().platform.backend_override).toBe("x11");
    await user.click(screen.getByRole("combobox", { name: "Platform backend" }));
    await user.click(await screen.findByRole("option", { name: "Automatic" }));
    expect(draft().platform.backend_override).toBeNull();
  });

  test("settings-changed adopts external settings when clean", async () => {
    await mount();
    await flush();
    await emitEvent("settings-changed", {
      settings: settings({
        ui: {
          language: "en",
          theme: "light",
          start_minimised: true,
          check_updates: true,
          onboarding_done: false,
          autostart: false,
        },
      }),
    });
    expect(screen.getByRole("switch", { name: "Start minimised to tray" })).toBeChecked();
  });

  test("settings-changed with unsaved edits offers to load the latest", async () => {
    const user = userEvent.setup();
    await mount();
    await flush();
    await user.click(screen.getByRole("tab", { name: "Overlay" }));
    const autoHide = screen.getByRole("textbox", { name: "Auto-hide delay (ms)" });
    fireEvent.change(autoHide, { target: { value: "3000" } });
    fireEvent.blur(autoHide);
    await emitEvent("settings-changed", {
      settings: settings({
        overlay: {
          auto_hide_ms: 2000,
          opacity: 1,
          side: "cursor",
          group_by_seller: true,
          listing_columns: ["seller", "listed"],
          scale: 1,
          backdrop_dim: 0.6,
          close_pinned_on_game_focus: false,
          links: { poe2db: true, wiki: true, craft_of_exile: true },
          results: "below",
        },
      }),
    });
    expect(screen.getByText("Settings were changed elsewhere.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load latest" }));
    expect(draft().overlay.auto_hide_ms).toBe(2000);
  });

  test("non-conflicting external changes merge into unsaved edits", async () => {
    const user = userEvent.setup();
    await mount();
    await flush();
    await user.click(screen.getByRole("switch", { name: "Start minimised to tray" }));
    await emitEvent("settings-changed", {
      settings: settings({
        ui: {
          language: "en",
          theme: "light",
          start_minimised: false,
          check_updates: true,
          onboarding_done: false,
          autostart: false,
        },
      }),
    });
    expect(screen.queryByText("Settings were changed elsewhere.")).toBeNull();
    expect(draft().ui).toMatchObject({
      theme: "light",
      start_minimised: true,
      check_updates: true,
      onboarding_done: false,
      autostart: false,
    });
  });
});

describe("hotkeys tab", () => {
  async function openHotkeys(handlers: Parameters<typeof setupIpc>[0] = {}) {
    const user = userEvent.setup();
    const res = await mount(handlers);
    await user.click(screen.getByRole("tab", { name: "Hotkeys" }));
    return { user, ...res };
  }

  test("clears a hotkey so it shows Not set and frees its chord", async () => {
    const { user, calls } = await openHotkeys();
    await user.click(screen.getByRole("button", { name: "Clear the Price check shortcut" }));
    await waitFor(() => expect(callsOf(calls, "rebind_hotkey")).toHaveLength(1));
    expect(callsOf(calls, "rebind_hotkey")[0]?.args).toEqual({
      action: "price_check",
      chord: { modifiers: [], key: "" },
    });
    expect(await screen.findByRole("button", { name: /^Price check Not set$/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear the Price check shortcut" })).toBeDisabled();
    expect(draft().hotkeys.price_check).toEqual({ modifiers: [], key: "" });
  });

  test("records a chord from keydown and rebinds it", async () => {
    const { user, calls } = await openHotkeys();
    const btn = screen.getByRole("button", { name: /^Price check Ctrl\+D$/ });
    await user.click(btn);
    expect(btn).toHaveTextContent("Press keys…");
    fireEvent.keyDown(btn, { code: "ControlLeft", ctrlKey: true });
    expect(btn).toHaveTextContent("Press keys…");
    fireEvent.keyDown(btn, { code: "KeyE", ctrlKey: true, shiftKey: true });
    await waitFor(() => expect(callsOf(calls, "rebind_hotkey")).toHaveLength(1));
    expect(callsOf(calls, "rebind_hotkey")[0]?.args).toEqual({
      action: "price_check",
      chord: { modifiers: ["ctrl", "shift"], key: "KeyE" },
    });
    expect(await screen.findByRole("button", { name: /^Price check Ctrl\+Shift\+E$/ })).toBeInTheDocument();
    expect(draft().hotkeys.price_check).toEqual({ modifiers: ["ctrl", "shift"], key: "KeyE" });
  });

  test("rebind persists without a false external-change banner, even with unsaved edits", async () => {
    const { user } = await openHotkeys();
    await flush();
    await user.click(screen.getByRole("switch", { name: /Advanced item copy/ }));
    const btn = screen.getByRole("button", { name: /^Open settings F10$/ });
    await user.click(btn);
    fireEvent.keyDown(btn, { code: "F9" });
    expect(await screen.findByRole("button", { name: /^Open settings F9$/ })).toBeInTheDocument();
    const st = useSettingsStore.getState();
    expect(st.base?.hotkeys.open_settings).toEqual({ modifiers: [], key: "F9" });
    expect(st.draft?.hotkeys.advanced_copy).toBe(true);
    expect(st.externalChange).toBe(false);
    expect(screen.queryByText("Settings were changed elsewhere.")).toBeNull();
  });

  test("needs a modifier for plain keys; Esc cancels", async () => {
    const { user, calls } = await openHotkeys();
    const btn = screen.getByRole("button", { name: /^Price check Ctrl\+D$/ });
    await user.click(btn);
    fireEvent.keyDown(btn, { code: "KeyQ" });
    expect(screen.getByRole("alert")).toHaveTextContent(/Add Ctrl, Alt, Shift or Meta/);
    fireEvent.keyDown(btn, { code: "Escape" });
    expect(btn).toHaveTextContent("Ctrl+D");
    expect(callsOf(calls, "rebind_hotkey")).toHaveLength(0);
    // Not recording: keys are ignored.
    fireEvent.keyDown(btn, { code: "KeyE", ctrlKey: true });
    expect(callsOf(calls, "rebind_hotkey")).toHaveLength(0);
  });

  test("rejects chords bound to another action without calling the backend", async () => {
    const { user, calls } = await openHotkeys();
    const btn = screen.getByRole("button", { name: /^Open settings F10$/ });
    await user.click(btn);
    fireEvent.keyDown(btn, { code: "KeyD", ctrlKey: true });
    expect(await screen.findByText("This shortcut is already used by another action.")).toBeInTheDocument();
    expect(callsOf(calls, "rebind_hotkey")).toHaveLength(0);
  });

  test("shows backend rebind failures", async () => {
    const { user } = await openHotkeys({ rebind_hotkey: () => fail(ipcError("platform", "grab failed")) });
    const btn = screen.getByRole("button", { name: /^Open settings F10$/ });
    await user.click(btn);
    fireEvent.keyDown(btn, { code: "F9" });
    expect(await screen.findByText("Couldn't bind that shortcut: grab failed")).toBeInTheDocument();
  });

  test("advanced copy toggle", async () => {
    const { user } = await openHotkeys();
    await user.click(screen.getByRole("switch", { name: /Advanced item copy/ }));
    expect(draft().hotkeys.advanced_copy).toBe(true);
  });

  test("clashing hotkeys in loaded settings show a validation error", async () => {
    const s = settings();
    await openHotkeys({
      get_settings: () => ({ ...s, hotkeys: { ...s.hotkeys, open_settings: s.hotkeys.price_check } }),
    });
    expect(screen.getByText("This shortcut is already used by another action.")).toBeInTheDocument();
  });
});
