// Package transport holds shared Connect client/server construction,
// interceptors, and the typed-error → Connect-code mapping at the handler
// boundary.
package transport

import (
	"errors"

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

// KindForConnectCode is the inverse of ConnectCodeFor: it recovers the typed
// giterr.Kind a Connect code most likely represents. It lets a Connect client
// adapter re-raise a remote failure as the same typed Kind the in-process impl
// would have returned, so consumers behind the core interface see one error
// vocabulary regardless of transport.
func KindForConnectCode(code connect.Code) giterr.Kind {
	switch code {
	case connect.CodePermissionDenied:
		return giterr.KindUnauthorized
	case connect.CodeNotFound:
		return giterr.KindRepoNotFound
	case connect.CodeFailedPrecondition:
		return giterr.KindRefRejected
	case connect.CodeUnavailable:
		return giterr.KindPlacementMiss
	default:
		return giterr.KindUnknown
	}
}

// FromConnectError maps a Connect client error back to a typed giterr.Error,
// preserving the original as the cause. A nil error returns nil; a non-Connect
// error is wrapped as KindUnknown.
func FromConnectError(err error) error {
	if err == nil {
		return nil
	}
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return giterr.Wrap(KindForConnectCode(cerr.Code()), err, "%s", cerr.Message())
	}
	return giterr.Wrap(giterr.KindUnknown, err, "%s", err.Error())
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
