// Package gitreq defines the normalized, transport-agnostic git request — the
// payload that crosses the Gateway→Repo Storage boundary (resolved repo UUID,
// operation enum, authenticated principal, authorization grant, protocol
// params, correlation id). Boundary types only, no heavy dependencies.
package gitreq

// Operation is the git wire operation crossing the boundary.
type Operation int

const (
	// OperationUnspecified is the zero value; a valid request never carries it.
	OperationUnspecified Operation = iota
	// OperationFetch is upload-pack (clone/fetch).
	OperationFetch
	// OperationReceive is receive-pack (push).
	OperationReceive
)

// GrantLevel is the decided permission level (the authorization grant). It is
// its own type so it can grow into a signed/verifiable assertion later without
// changing the call shape; the slice carries a simple resolved level.
type GrantLevel int

const (
	// GrantLevelUnspecified is the zero value (no access decided).
	GrantLevelUnspecified GrantLevel = iota
	GrantLevelRead
	GrantLevelWrite
	GrantLevelAdmin
)

// Principal is the authenticated identity carried for attribution and hooks
// (reflog, future provenance). It is NEVER consulted to allow or deny — the
// Gateway already decided that and recorded it in Grant.
type Principal struct {
	// UserID is the internal user UUID; empty when Anonymous.
	UserID string
	// Anonymous is true for an unauthenticated public fetch.
	Anonymous bool
}

// Grant is the authorization decision made at the Gateway. Repo Storage trusts
// it and does not re-evaluate user permissions.
type Grant struct {
	Level GrantLevel
}

// ProtocolParams carries the negotiated git wire protocol.
type ProtocolParams struct {
	// Version is 2 (preferred) or 0 (fallback).
	Version      int
	Capabilities []string
}

// GitRequest is the normalized, transport-agnostic payload. Nothing in it
// reveals SSH vs HTTPS.
type GitRequest struct {
	// RepoID is the resolved internal repo UUID (the Gateway resolved it via the
	// Registry; Repo Storage does not re-parse owner/repo).
	RepoID    string
	Operation Operation
	Principal Principal
	Grant     Grant
	Protocol  ProtocolParams
	// CorrelationID is threaded from the edge for cross-boundary tracing.
	CorrelationID string
}
