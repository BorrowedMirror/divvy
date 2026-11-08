// Package bindgen writes web/src/bindings.ts, the frontend's typed view of the Go backend:
// its `commands`, `events` and types, calling Wails.
//
// The generated file follows a fixed contract with the web views, which import its names and
// shapes directly:
//
//   - Commands are the exported methods of one bound service, in source order. Each becomes
//     `commands.<lowerCamelName>(params...)` and is sent under its command name, the method
//     name in snake_case (`GetSettings` -> `get_settings`); the transport, the test mocks and
//     the browser preview are keyed by these names. A leading `context.Context` parameter is
//     supplied by Wails and is not part of the TypeScript signature.
//   - A method returning `(T, error)` or just `error` resolves to `{status: "ok", data}` or
//     `{status: "error", error}`, the error typed as [Spec.Error]; a method without an error
//     result resolves to its value.
//   - Events become `events.<name>.listen(cb)`, where cb receives `{event, payload}`.
//   - A struct type is declared three times: `T_Serialize` (what the backend sends: results
//     and event payloads), `T_Deserialize` (what it accepts: parameters, where fields the
//     backend fills in when missing are optional) and `T`, either of them.
//   - A string type is the union of its constants, each constant's doc comment documenting
//     its member.
//
// Types are generated from the Go types by reflection, using their `json` tags; a
// `ts:"type=..."` tag overrides a field's TypeScript type and `ts:"optional"` makes it
// optional when sent. [Spec.Names] renames a type for the frontend. Types whose JSON is
// written by hand (tagged unions) have no Go shape reflection can see: their TypeScript goes
// in [Spec.Literal]. Doc comments, parameter names and constants are read from the Go source
// a default build compiles.
package bindgen
