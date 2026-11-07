// Command bindgen writes web/src/bindings.ts from the Go IPC service and event types.
//
//	go run ./cmd/bindgen          # write the file
//	go run ./cmd/bindgen -check   # fail if the file is out of date (CI)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/bindgen"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
)

func main() {
	check := flag.Bool("check", false, "fail if web/src/bindings.ts is out of date instead of writing it")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, "bindgen:", err)
		os.Exit(1)
	}
}

func run(check bool) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	out, err := bindgen.Generate(bindgen.Spec{
		Service:    reflect.TypeFor[app.Service](),
		ModulePath: "github.com/BorrowedMirror/rust-king-ledger",
		ModuleDir:  root,
		Literal:    literal(),
		Events: []bindgen.Event{
			{Name: "itemCopied", Wire: ipc.EventItemCopied, Payload: reflect.TypeFor[ipc.ItemCopied]()},
			{Name: "overlayLayout", Wire: ipc.EventOverlayLayout, Payload: reflect.TypeFor[ipc.OverlayLayout]()},
			{Name: "overlayState", Wire: ipc.EventOverlayState, Payload: reflect.TypeFor[ipc.OverlayState]()},
			{Name: "permissionsChanged", Wire: ipc.EventPermissionsChanged, Payload: reflect.TypeFor[app.PermissionsChanged]()},
			{Name: "priceFailed", Wire: ipc.EventPriceFailed, Payload: reflect.TypeFor[ipc.PriceFailed]()},
			{Name: "priceReady", Wire: ipc.EventPriceReady, Payload: reflect.TypeFor[ipc.PriceReady]()},
			{Name: "settingsChanged", Wire: ipc.EventSettingsChanged, Payload: reflect.TypeFor[ipc.SettingsChanged]()},
			{Name: "updateAvailable", Wire: ipc.EventUpdateAvailable, Payload: reflect.TypeFor[ipc.UpdateAvailable]()},
		},
	})
	if err != nil {
		return err
	}
	path := filepath.Join(root, "web", "src", "bindings.ts")
	if check {
		cur, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(cur, out) {
			return fmt.Errorf("%s is out of date; run `go run ./cmd/bindgen`", path)
		}
		return nil
	}
	return os.WriteFile(path, out, 0o644)
}

// literal merges the hand-written TypeScript for enums and unions from internal/ipc (core,
// platform, config and IPC types) and internal/app (the app's own types).
func literal() map[reflect.Type]string {
	m := make(map[reflect.Type]string, len(ipc.Literal)+3)
	for t, ts := range ipc.Literal {
		m[t] = ts
	}
	for t, ts := range app.Literal {
		m[t] = ts
	}
	return m
}

func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		return "", fmt.Errorf("not inside the Go module")
	}
	return filepath.Dir(mod), nil
}
