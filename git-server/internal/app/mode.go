// Package app is the wiring root — the ONLY mode-aware package.
//
// Mode-awareness must never leak below internal/app/; the standing
// no-mode-branching grep enforces this. For now this package only knows how to
// parse the runtime role so the entrypoint can validate --mode and exit.
package app

import "fmt"

// Mode is the runtime role the single artifact launches into.
type Mode string

const (
	ModeGateway     Mode = "gateway"
	ModeRepoStorage Mode = "repo-storage"
	ModeCache       Mode = "cache"
	ModeRegistry    Mode = "registry"
	ModeAll         Mode = "all"
)

// ParseMode validates s against the known roles.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeGateway, ModeRepoStorage, ModeCache, ModeRegistry, ModeAll:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("invalid mode %q: want one of gateway|repo-storage|cache|registry|all", s)
	}
}
