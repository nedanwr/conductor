// Command git-server is the sole entrypoint for the git server.
//
// It loads configuration, parses the runtime role (--mode) and the admin
// subcommand, then hands off to internal/app. There is a single artifact; the
// binary launches into a role at runtime rather than shipping as separate
// per-service binaries.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nedanwr/conductor/git-server/internal/app"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "git-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("git-server", flag.ContinueOnError)
	modeStr := fs.String("mode", "all", "runtime role: gateway|repo-storage|cache|registry|all")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mode, err := app.ParseMode(*modeStr)
	if err != nil {
		return err
	}

	// No services are wired yet. Acknowledge the selected mode and exit.
	fmt.Printf("git-server: mode=%s (no services wired yet)\n", mode)
	return nil
}
