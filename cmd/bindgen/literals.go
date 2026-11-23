package main

import (
	"reflect"

	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

// literal is the TypeScript of the tagged unions, whose JSON is written by hand (a string per
// plain variant, an object per variant with data) and has no Go shape the generator can read.
// Edit it when one of these types changes.
var literal = map[reflect.Type]string{
	reflect.TypeFor[platform.HotkeyAction](): `"price_check" | "price_check_pinned" | "open_settings" | "open_trade" | ` +
		"\n/**  A chat command hotkey (index into the configured chat commands). */\n" +
		`({ chat: number }) & { stash_search?: never } | ` +
		"\n/**  A stash search hotkey (index into the configured stash searches). */\n" +
		`({ stash_search: number }) & { chat?: never }`,
	reflect.TypeFor[core.ItemClass](): `{ type: "stackable_currency" } | { type: "omen" } | { type: "essence" } | ` +
		`{ type: "soul_core" } | { type: "rune" } | { type: "talisman" } | { type: "distilled_emotion" } | ` +
		`{ type: "splinter" } | { type: "catalyst" } | { type: "skill_gem" } | { type: "support_gem" } | ` +
		`{ type: "uncut_gem" } | { type: "waystone" } | { type: "tablet" } | { type: "jewel" } | ` +
		`{ type: "life_flask" } | { type: "mana_flask" } | { type: "charm" } | { type: "relic" } | ` +
		`{ type: "amulet" } | { type: "ring" } | { type: "belt" } | { type: "body_armour" } | ` +
		`{ type: "helmet" } | { type: "gloves" } | { type: "boots" } | { type: "shield" } | ` +
		`{ type: "buckler" } | { type: "focus" } | { type: "quiver" } | ` +
		"\n/**  Weapon subclasses (\"Bows\", \"Two Hand Maces\", ...), raw header value preserved. */\n" +
		`{ type: "weapon"; value: string } | ` +
		"\n/**  Unmapped header value preserved verbatim. */\n" +
		`{ type: "other"; value: string }`,
}
