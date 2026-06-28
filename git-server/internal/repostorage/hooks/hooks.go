// Package hooks holds the server-side git hook templates run by receive-pack and
// installs them into a bare repo. The templates are the seam where rejection
// rules — and, later, provenance emission — run inside the push path.
package hooks

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

//go:embed templates/*
var templates embed.FS

// Install writes every hook template into <repoPath>/hooks, overwriting any
// existing copy so a repo always carries the current server hooks. Hooks are
// written executable; receive-pack ignores a non-executable hook.
func Install(repoPath string) error {
	hookDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "create hook dir for %s", repoPath)
	}

	entries, err := fs.ReadDir(templates, "templates")
	if err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "read embedded hook templates")
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := templates.ReadFile("templates/" + e.Name())
		if err != nil {
			return giterr.Wrap(giterr.KindGitExec, err, "read hook template %s", e.Name())
		}
		dst := filepath.Join(hookDir, e.Name())
		if err := os.WriteFile(dst, body, 0o755); err != nil {
			return giterr.Wrap(giterr.KindGitExec, err, "write hook %s", dst)
		}
	}
	return nil
}
