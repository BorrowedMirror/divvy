// Command datarefresh regenerates the bundled game data in internal/data/assets/ and records
// the trade API test fixtures.
//
//	go run ./cmd/datarefresh refresh [-offline]   # download, normalise and write the assets
//	go run ./cmd/datarefresh l10n <raw.json>      # rebuild the translations (see below)
//	go run ./cmd/datarefresh fixtures [flags]     # re-record internal/trade/testdata/fixtures/
//
// Translations come from an installed game client, whose files are Oodle-compressed. The
// standalone tool in tools/game-extract (Rust, for its Oodle bindings) reads them and writes
// the raw tables as JSON (`game-extract l10n <game_dir> <raw.json>`); the `l10n` command
// builds the asset from that file. Sources are documented in docs/research/data-sources.md.
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
	"time"
)

// editorconfig-checker-disable
const usage = `usage: go run ./cmd/datarefresh <command> [flags]

commands:
  refresh [-offline]   download the trade2 data and RePoE exports (cached in target/data-cache),
                       normalise them and write internal/data/assets
  l10n <raw.json>      rebuild the translations for non-English clients from the output of
                       tools/game-extract (game-extract l10n <game_dir> <raw.json>)
  fixtures [flags]     re-record the trade2 fixtures in internal/trade/testdata/fixtures from the
                       live API, 2 s between requests

Run it from the repository root. Run "go run ./cmd/datarefresh <command> -h" for the flags of
a command.
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
		fmt.Fprintln(os.Stderr, "datarefresh:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command")
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "refresh":
		return runRefresh(ctx, rest)
	case "l10n":
		return runL10n(rest)
	case "fixtures":
		return runFixtures(ctx, rest)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// moduleRoot is the working directory, which must be the module root: `go run
// ./cmd/datarefresh` runs from there.
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

// sleep waits for d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
