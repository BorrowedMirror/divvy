// Command xtask holds the Go build's maintenance tasks: checks, coverage and packaging.
//
//	go run ./cmd/xtask check              # gofmt, vet, tests, cross-vet, bindgen -check
//	go run ./cmd/xtask coverage           # go test -race with per-package coverage profiles
//	go run ./cmd/xtask package linux      # AppImage        -> target/go/
//	go run ./cmd/xtask package windows    # NSIS installer  -> target/go/ (cross-compiled)
//	go run ./cmd/xtask package macos      # .app + DMG      -> target/go/ (macOS only)
//	go run ./cmd/xtask version            # print the app version
//
// The web UI is embedded into the binary, so build it first: `pnpm --dir web build`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = `usage: go run ./cmd/xtask <command> [flags]

commands:
  check                    gofmt, go vet (host, windows, darwin), go test, bindgen -check, golangci-lint
  coverage                 go test -race with coverage, split per package into target/go/coverage/
  package linux|windows|macos
                           build the release binary and its installer under target/go/
  updater-keygen           print a new updater key pair (RKL_UPDATER_PUBKEY / RKL_UPDATER_PRIVATE_KEY)
  update-manifest          sign the update artifacts and write update.json (needs RKL_UPDATER_PRIVATE_KEY)
  version                  print the app version

Run "go run ./cmd/xtask <command> -h" for the flags of a command.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "xtask:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "check":
		return runCheck(root, rest)
	case "coverage":
		return runCoverage(root, rest)
	case "package":
		return runPackage(root, rest)
	case "updater-keygen":
		return runUpdaterKeygen(stdout)
	case "update-manifest":
		return runUpdateManifest(rest, stdout)
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

func runPackage(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("package: name a target: linux, windows or macos")
	}
	target, rest := args[0], args[1:]
	fs := flag.NewFlagSet("package "+target, flag.ContinueOnError)
	version := fs.String("version", "", "app version (default: from "+versionFile+")")
	if err := fs.Parse(rest); err != nil {
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
		return p.linux()
	case "windows":
		return p.windows()
	case "macos":
		return p.macos()
	default:
		return fmt.Errorf("package: unknown target %q (linux, windows, macos)", target)
	}
}
