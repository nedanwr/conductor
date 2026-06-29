// Command git-server is the sole entrypoint for the git server.
//
// It loads configuration, parses the runtime role (--mode) and the admin
// subcommand, then hands off to internal/app. There is a single artifact; the
// binary launches into a role at runtime rather than shipping as separate
// per-service binaries.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nedanwr/conductor/git-server/internal/app"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "git-server:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	// admin is a run-and-exit verb, not a service role; it is dispatched before
	// the --mode flag is considered.
	if len(args) > 0 && args[0] == "admin" {
		return app.Admin(ctx, args[1:])
	}

	fs := flag.NewFlagSet("git-server", flag.ContinueOnError)
	modeStr := fs.String("mode", "all", "runtime role: gateway|repo-storage|cache|registry|all")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mode, err := app.ParseMode(*modeStr)
	if err != nil {
		return err
	}

	return app.Run(ctx, app.LoadConfig(mode))
}
