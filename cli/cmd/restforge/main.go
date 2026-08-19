// Command restforge is a generic Siren hypermedia browser for the terminal —
// the Go/Charm sibling of ../../pebble and ../../flutter. See ../../AGENTS.md
// and ../../README.md for how the three implementations relate.
//
// main.go is intentionally tiny: it hands os.Args to internal/cli, which owns
// the Cobra command tree, the mode-by-invocation dispatch (no subcommand →
// interactive TUI; any subcommand → one-shot with an exit code), and the
// global flags. Keeping this file to argument plumbing is the same split
// cli/AGENTS.md and internal/cli's package comment describe.
package main

import (
	"os"

	"github.com/snonux/restforge/cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
