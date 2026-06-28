// Package transport holds shared Connect client/server construction,
// interceptors, and the typed-error → Connect-code mapping at the handler
// boundary.
package transport

import (
	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// ConnectCodeFor maps a typed giterr.Kind to its Connect code. This is
// the single place the boundary translates domain errors to wire codes; handlers
// call AsConnectError. The mapping:
//
//	Unauthorized  → permission_denied
//	RepoNotFound  → not_found
//	RefRejected   → failed_precondition
//	PlacementMiss → unavailable
//	GitExecError  → internal
//	DBError       → internal
//	Unknown       → unknown
func ConnectCodeFor(kind giterr.Kind) connect.Code {
	switch kind {
	case giterr.KindUnauthorized:
		return connect.CodePermissionDenied
	case giterr.KindRepoNotFound:
		return connect.CodeNotFound
	case giterr.KindRefRejected:
		return connect.CodeFailedPrecondition
	case giterr.KindPlacementMiss:
		return connect.CodeUnavailable
	case giterr.KindGitExec:
		return connect.CodeInternal
	case giterr.KindDB:
		return connect.CodeInternal
	default:
		return connect.CodeUnknown
	}
}

// AsConnectError converts err into a *connect.Error with the code mapped from
// its typed Kind. A nil error returns nil. The original error is preserved as
// the connect.Error message/cause so attribution survives the boundary.
func AsConnectError(err error) *connect.Error {
	if err == nil {
		return nil
	}
	return connect.NewError(ConnectCodeFor(giterr.KindOf(err)), err)
}
