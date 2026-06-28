package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
)

// Authorizer is the single authorization source of truth. Nothing else in the
// system decides whether a principal may fetch or receive.
type Authorizer struct {
	store *auth.Store
}

// NewAuthorizer builds an Authorizer over the auth store.
func NewAuthorizer(store *auth.Store) *Authorizer {
	return &Authorizer{store: store}
}

// Can decides whether principal may perform op on the repo and, on success,
// returns the authorization grant the Gateway carries to Repo Storage. A denial
// is a typed Unauthorized error. An anonymous principal carries no permission
// level; it is admitted only by public-repo fetch.
func (a *Authorizer) Can(ctx context.Context, principal auth.User, repoID uuid.UUID, op gitreq.Operation) (gitreq.Grant, error) {
	vis, err := a.store.Visibility(ctx, repoID)
	if err != nil {
		return gitreq.Grant{}, err
	}

	level := auth.LevelNone
	if !principal.Anonymous {
		level, err = a.store.LevelFor(ctx, principal.ID, repoID)
		if err != nil {
			return gitreq.Grant{}, err
		}
	}

	return decide(op, vis, level)
}

// decide is the pure authorization rule, factored out so the truth table is
// testable without a database. The rules: receive always requires write or
// above; fetch requires read or above, except a public repo grants read to
// anyone (including the anonymous principal).
func decide(op gitreq.Operation, vis auth.Visibility, level auth.Level) (gitreq.Grant, error) {
	switch op {
	case gitreq.OperationReceive:
		if level >= auth.LevelWrite {
			return gitreq.Grant{Level: grantLevel(level)}, nil
		}
		return gitreq.Grant{}, giterr.Unauthorized("push denied: write permission required")

	case gitreq.OperationFetch:
		if level >= auth.LevelRead {
			return gitreq.Grant{Level: grantLevel(level)}, nil
		}
		if vis == auth.VisibilityPublic {
			return gitreq.Grant{Level: gitreq.GrantLevelRead}, nil
		}
		return gitreq.Grant{}, giterr.Unauthorized("fetch denied: read permission required")

	default:
		return gitreq.Grant{}, giterr.Unauthorized("unsupported operation")
	}
}

// grantLevel maps a stored permission Level to the boundary GrantLevel.
func grantLevel(l auth.Level) gitreq.GrantLevel {
	switch l {
	case auth.LevelAdmin:
		return gitreq.GrantLevelAdmin
	case auth.LevelWrite:
		return gitreq.GrantLevelWrite
	case auth.LevelRead:
		return gitreq.GrantLevelRead
	default:
		return gitreq.GrantLevelUnspecified
	}
}
