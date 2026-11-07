package app

import "reflect"

// Literal is the TypeScript for this package's string enums, which the bindings generator
// cannot derive from the Go types (see internal/ipc.Literal for the other packages). First
// extracted from the tauri-specta bindings.ts; edit by hand when a type changes.
var Literal = map[reflect.Type]string{
	reflect.TypeFor[LookupSite]():       "\"poe2db\" | \"wiki\" | \"craft_of_exile\"",
	reflect.TypeFor[WaystoneModGroup](): "\"player\" | \"monster\" | \"area\"",
}
