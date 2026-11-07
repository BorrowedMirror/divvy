// Command datarefresh regenerates the bundled game data in internal/data/assets/ and records
// the trade API test fixtures.
//
//	go run ./cmd/datarefresh refresh [-offline]   # download, normalise and write the assets
//	go run ./cmd/datarefresh l10n <raw.json>      # rebuild the translations (see below)
//	go run ./cmd/datarefresh fixtures [flags]     # re-record internal/trade/testdata/fixtures/
//
// Translations come from an installed game client, whose files are Oodle-compressed. The
// standalone Rust tool in tools/game-extract reads them and writes the raw tables as JSON
// (`game-extract l10n <game_dir> <raw.json>`); the `l10n` command builds the asset from
// that file. Sources are documented in docs/research/data-sources.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = `usage: go run ./cmd/datarefresh <command> [flags]

commands:
  refresh [-offline]   download the trade2 data and RePoE exports (cached in target/data-cache),
                       normalise them and write internal/data/assets
  l10n <raw.json>      rebuild the translations for non-English clients from the output of
                       tools/game-extract (game-extract l10n <game_dir> <raw.json>)
  fixtures [flags]     re-record the trade2 fixtures in internal/trade/testdata/fixtures from the
                       live API, 2 s between requests

Run "go run ./cmd/datarefresh <command> -h" for the flags of a command.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "datarefresh:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command")
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "refresh":
		return runRefresh(rest)
	case "l10n":
		return runL10n(rest)
	case "fixtures":
		return runFixtures(rest)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// repoRoot is the nearest directory at or above the working directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside the rkl repository (no go.mod above the working directory)")
		}
		dir = parent
	}
}

// logf reports progress on stderr.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
