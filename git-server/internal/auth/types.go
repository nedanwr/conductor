// Package auth is the authentication and authorization concern for the git
// path: the single authZ source of truth. It is consumed locally, not exposed
// as a Connect service. Authentication (credential → user) lives in the user
// subpackage alongside the sole Can(user, repo, action) evaluator.
package auth

import (
	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// Level is a user's permission level on a repo. The ordering is meaningful:
// admin implies write implies read, so comparisons (level >= LevelWrite) decide
// access.
type Level int

const (
	// LevelNone means no permission row exists for the (user, repo) pair.
	LevelNone Level = iota
	LevelRead
	LevelWrite
	LevelAdmin
)

// String returns the stored token for the level (matches the DB CHECK values).
func (l Level) String() string {
	switch l {
	case LevelRead:
		return "read"
	case LevelWrite:
		return "write"
	case LevelAdmin:
		return "admin"
	default:
		return "none"
	}
}

// ParseLevel maps a stored level string to a Level. An unrecognized value is a
// DB integrity error.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "read":
		return LevelRead, nil
	case "write":
		return LevelWrite, nil
	case "admin":
		return LevelAdmin, nil
	default:
		return LevelNone, giterr.DB("unknown permission level %q", s)
	}
}

// Visibility is a repo's visibility. Public repos allow anonymous fetch.
type Visibility int

const (
	// VisibilityPrivate requires at least read permission to fetch.
	VisibilityPrivate Visibility = iota
	// VisibilityPublic allows anonymous fetch; receive still needs write.
	VisibilityPublic
)

// String returns the stored visibility string (matches the DB CHECK values).
func (v Visibility) String() string {
	if v == VisibilityPublic {
		return "public"
	}
	return "private"
}

// ParseVisibility maps a stored visibility string to a Visibility. An
// unrecognized value is a DB integrity error.
func ParseVisibility(s string) (Visibility, error) {
	switch s {
	case "private":
		return VisibilityPrivate, nil
	case "public":
		return VisibilityPublic, nil
	default:
		return VisibilityPrivate, giterr.DB("unknown visibility %q", s)
	}
}

// User is an authenticated identity. The zero value with Anonymous set is the
// unauthenticated principal used for public fetch.
type User struct {
	ID       uuid.UUID
	Username string
	// Anonymous is true for the unauthenticated principal.
	Anonymous bool
}

// Anonymous is the sentinel unauthenticated principal.
var Anonymous = User{Anonymous: true}
