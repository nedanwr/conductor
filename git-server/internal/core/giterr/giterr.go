// Package giterr defines the typed-error set for the git server — no untyped
// errors on the boundary. Errors carry a Kind; the handler boundary maps Kind to
// a Connect code (see internal/transport). The set is extensible: add a Kind and
// a mapping.
package giterr

import (
	"errors"
	"fmt"
)

// Kind classifies a boundary error. The zero value KindUnknown maps to an
// internal Connect code.
type Kind int

const (
	KindUnknown Kind = iota
	// KindUnauthorized: authN failed or authZ denied.
	KindUnauthorized
	// KindRepoNotFound: the repo does not exist / cannot be resolved.
	KindRepoNotFound
	// KindRefRejected: a ref update was rejected by a rule or hook.
	KindRefRejected
	// KindPlacementMiss: no placement for the repo.
	KindPlacementMiss
	// KindGitExec: a shelled git process failed.
	KindGitExec
	// KindDB: a Postgres/query error.
	KindDB
)

// String returns the canonical name of the Kind.
func (k Kind) String() string {
	switch k {
	case KindUnauthorized:
		return "Unauthorized"
	case KindRepoNotFound:
		return "RepoNotFound"
	case KindRefRejected:
		return "RefRejected"
	case KindPlacementMiss:
		return "PlacementMiss"
	case KindGitExec:
		return "GitExecError"
	case KindDB:
		return "DBError"
	default:
		return "Unknown"
	}
}

// Error is a typed boundary error. It wraps an optional cause for errors.Is/As.
type Error struct {
	Kind Kind
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Msg == "" {
		return e.Kind.String()
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Msg)
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

// New builds a typed error of the given kind.
func New(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Wrap builds a typed error wrapping cause.
func Wrap(kind Kind, cause error, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...), Err: cause}
}

// KindOf reports the Kind of err, unwrapping the chain. A nil error or one that
// is not a *Error reports KindUnknown.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// Constructors for each variant.

func Unauthorized(format string, args ...any) *Error  { return New(KindUnauthorized, format, args...) }
func RepoNotFound(format string, args ...any) *Error  { return New(KindRepoNotFound, format, args...) }
func RefRejected(format string, args ...any) *Error   { return New(KindRefRejected, format, args...) }
func PlacementMiss(format string, args ...any) *Error { return New(KindPlacementMiss, format, args...) }
func GitExec(format string, args ...any) *Error       { return New(KindGitExec, format, args...) }
func DB(format string, args ...any) *Error            { return New(KindDB, format, args...) }
