// Literal TypeScript, first extracted from the tauri-specta bindings.ts; edit by hand when a
// type changes.

package ipc

import (
	"reflect"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Literal is the TypeScript for types whose JSON is written by hand (string enums and
// serde-style tagged unions), which the bindings generator cannot derive from the Go type.
// The text, doc comments included, is what tauri-specta generated from the Rust types.
var Literal = map[reflect.Type]string{
	reflect.TypeFor[core.Affix]():            "\"prefix\" | \"suffix\" | \"implicit\"",
	reflect.TypeFor[platform.BackendKind]():  "\"windows\" | \"macos\" | \n/**  X11 or XWayland (default on Linux). */\n\"x11\" | \n/**  Wayland-native (layer-shell + portals). */\n\"wayland\" | \n/**  In-process fakes (tests, `RKL_BACKEND=fake`). */\n\"fake\"",
	reflect.TypeFor[DragPhase]():             "\"start\" | \"move\" | \"end\"",
	reflect.TypeFor[platform.HotkeyAction](): "\"price_check\" | \"price_check_pinned\" | \"open_settings\" | \"open_trade\" | \n/**  A chat command hotkey (index into the configured chat commands). */\n({ chat: number }) & { stash_search?: never } | \n/**  A stash search hotkey (index into the configured stash searches). */\n({ stash_search: number }) & { chat?: never }",
	reflect.TypeFor[core.ItemCategory]():     "\"currency\" | \"gem\" | \"unique\" | \"equipment\" | \"waystone\" | \"tablet\" | \"jewel\" | \"flask\" | \"charm\" | \"relic\" | \"other\"",
	reflect.TypeFor[core.ItemClass]():        "{ type: \"stackable_currency\" } | { type: \"omen\" } | { type: \"essence\" } | { type: \"soul_core\" } | { type: \"rune\" } | { type: \"talisman\" } | { type: \"distilled_emotion\" } | { type: \"splinter\" } | { type: \"catalyst\" } | { type: \"skill_gem\" } | { type: \"support_gem\" } | { type: \"uncut_gem\" } | { type: \"waystone\" } | { type: \"tablet\" } | { type: \"jewel\" } | { type: \"life_flask\" } | { type: \"mana_flask\" } | { type: \"charm\" } | { type: \"relic\" } | { type: \"amulet\" } | { type: \"ring\" } | { type: \"belt\" } | { type: \"body_armour\" } | { type: \"helmet\" } | { type: \"gloves\" } | { type: \"boots\" } | { type: \"shield\" } | { type: \"buckler\" } | { type: \"focus\" } | { type: \"quiver\" } | \n/**  Weapon subclasses (\"Bows\", \"Two Hand Maces\", ...), raw header value preserved. */\n{ type: \"weapon\"; value: string } | \n/**  Unmapped header value preserved verbatim. */\n{ type: \"other\"; value: string }",
	reflect.TypeFor[platform.KeyModifier]():  "\"ctrl\" | \"alt\" | \"shift\" | \"meta\"",
	reflect.TypeFor[config.ListingColumn]():  "\"seller\" | \"item_level\" | \"stock\" | \"listed\"",
	reflect.TypeFor[config.ModFlag]():        "\"danger\" | \"warn\" | \"good\"",
	reflect.TypeFor[core.ModKind]():          "\"implicit\" | \"explicit\" | \"enchant\" | \"rune\" | \"crafted\" | \"fractured\" | \"desecrated\" | \"sanctified\" | \"skill\"",
	reflect.TypeFor[core.OnlineStatus]():     "\n/**  `securable` (instant buyout) in trade2. */\n\"securable\" | \"online\" | \"any\"",
	reflect.TypeFor[config.OverlaySide]():    "\n/**  Beside the cursor, flipping when near the screen edge. */\n\"cursor\" | \"left\" | \"right\" | \n/**  Where the user last dragged the panel (beside the cursor until it has been moved). */\n\"last\"",
	reflect.TypeFor[core.PriceSourceKind]():  "\"trade\" | \"poe_ninja\" | \"poe2_scout\"",
	reflect.TypeFor[core.QueryRoute]():       "\n/**  `/api/trade2/search/{league}` */\n\"search\" | \n/**  `/api/trade2/exchange/{league}` (bulk currency). */\n\"exchange\"",
	reflect.TypeFor[core.Rarity]():           "\"normal\" | \"magic\" | \"rare\" | \"unique\" | \"currency\" | \"gem\" | \n/**  Quest items, divination-like or other. */\n\"other\"",
	reflect.TypeFor[config.ResultsLayout]():  "\n/**  Under the Search button (one narrow column). */\n\"below\" | \n/**  In a column to the left of the filters. */\n\"left\" | \n/**  In a column to the right of the filters. */\n\"right\" | \n/**  Beside the filters (right) for items with many stats, below otherwise. */\n\"auto\"",
	reflect.TypeFor[config.Theme]():          "\"system\" | \"dark\" | \"light\"",
}
