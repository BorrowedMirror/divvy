// Package ipc defines the boundary between the Go backend and the web views: the error every
// command fails with ([Error]), the commands' request and result types, and the events the
// backend emits with their payloads. Every type here marshals to the JSON the frontend expects
// (see wire.go); `go generate ./...` turns them, with the commands bound by app.Service, into
// web/src/bindings.ts. The behaviour lives in internal/app.
package ipc
