// Command bindgen writes web/src/bindings.ts from the bound Go service (app.Service) and the
// internal/ipc types. It runs from a go:generate line on app.Service.
//
//	go generate ./...             # write the file
//	go run ./cmd/bindgen -check   # fail if the file is out of date (CI)
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BorrowedMirror/divvy/internal/app"
	"github.com/BorrowedMirror/divvy/internal/bindgen"
	"github.com/BorrowedMirror/divvy/internal/ipc"
)

func main() {
	root := flag.String("root", ".", "module root (holding go.mod)")
	check := flag.Bool("check", false, "fail if web/src/bindings.ts is out of date instead of writing it")
	flag.Parse()
	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, "bindgen:", err)
		os.Exit(1)
	}
}

func run(root string, check bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return errors.New("-root must be the module root (no go.mod there)")
	}
	out, err := bindgen.Generate(bindgen.Spec{
		Service:    reflect.TypeFor[app.Service](),
		ModulePath: "github.com/BorrowedMirror/divvy",
		ModuleDir:  root,
		Error:      reflect.TypeFor[ipc.Error](),
		// The frontend imports the command error as IpcError.
		Names:   map[reflect.Type]string{reflect.TypeFor[ipc.Error](): "IpcError"},
		Literal: literal,
		Events: []bindgen.Event{
			{Name: "itemCopied", Wire: ipc.EventItemCopied, Payload: reflect.TypeFor[ipc.ItemCopied]()},
			{Name: "overlayLayout", Wire: ipc.EventOverlayLayout, Payload: reflect.TypeFor[ipc.OverlayLayout]()},
			{Name: "overlayState", Wire: ipc.EventOverlayState, Payload: reflect.TypeFor[ipc.OverlayState]()},
			{Name: "permissionsChanged", Wire: ipc.EventPermissionsChanged, Payload: reflect.TypeFor[ipc.PermissionsChanged]()},
			{Name: "priceFailed", Wire: ipc.EventPriceFailed, Payload: reflect.TypeFor[ipc.PriceFailed]()},
			{Name: "priceReady", Wire: ipc.EventPriceReady, Payload: reflect.TypeFor[ipc.PriceReady]()},
			{Name: "readyNotice", Wire: ipc.EventReadyNotice, Payload: reflect.TypeFor[ipc.ReadyNotice]()},
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
			return fmt.Errorf("%s is out of date; run `go generate ./...`", path)
		}
		return nil
	}
	return os.WriteFile(path, out, 0o644)
}
