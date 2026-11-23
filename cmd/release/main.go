// Command release packages Divvy and signs its updates: the steps of a release that the go
// command cannot do on its own. Taskfile.yml runs it (task package:linux, updater:manifest, ...).
//
//	go run ./cmd/release package linux      # AppImage        -> target/go/
//	go run ./cmd/release package windows    # NSIS installer  -> target/go/ (cross-compiled)
//	go run ./cmd/release package macos      # .app + DMG      -> target/go/ (macOS only)
//	go run ./cmd/release notices            # third-party notices -> target/go/
//	go run ./cmd/release version            # print the app version
//
// The web UI is embedded into the binary, so build it first: `pnpm --dir web build`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
)

// editorconfig-checker-disable
const usage = `usage: go run ./cmd/release <command> [flags]

commands:
  package linux|windows|macos
                           build the release binary and its installer under target/go/
  updater-keygen           print a new updater key pair (DIVVY_UPDATER_PUBKEY / DIVVY_UPDATER_PRIVATE_KEY)
  update-manifest          sign the update artifacts and write update.json (needs DIVVY_UPDATER_PRIVATE_KEY)
  notices [-check]         write THIRD-PARTY-NOTICES.html (or only check the licence policy)
  version                  print the app version

Run it from the repository root. Run "go run ./cmd/release <command> -h" for the flags of a
command.
`

// editorconfig-checker-enable

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "package":
		return runPackage(ctx, root, rest)
	case "updater-keygen":
		return runUpdaterKeygen(stdout)
	case "update-manifest":
		return runUpdateManifest(rest, stdout)
	case "notices":
		return runNotices(ctx, root, rest)
	case "version":
		v, err := appVersion(root)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, v)
		return err
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// moduleRoot is the working directory, which must be the module root: `go run ./cmd/release`
// runs from there.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return "", errors.New("run from the repository root (no go.mod in the working directory)")
	}
	return dir, nil
}

func runPackage(ctx context.Context, root string, args []string) error {
	if len(args) == 0 {
		return errors.New("package: name a target: linux, windows or macos")
	}
	target, rest := args[0], args[1:]
	flags := flag.NewFlagSet("package "+target, flag.ContinueOnError)
	version := flags.String("version", os.Getenv(versionEnv), "app version (default: $"+versionEnv+", else "+versionFile+")")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	v := *version
	if v == "" {
		var err error
		if v, err = appVersion(root); err != nil {
			return err
		}
	}
	if err := validateVersion(v); err != nil {
		return err
	}
	p := packager{root: root, version: v}
	switch target {
	case "linux":
		return p.linux(ctx)
	case "windows":
		return p.windows(ctx)
	case "macos":
		return p.macos(ctx)
	default:
		return fmt.Errorf("package: unknown target %q (linux, windows, macos)", target)
	}
}
