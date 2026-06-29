package service

import (
	"context"
	"time"
)

// EnrollParams is a node's request to join the deployment: the bootstrap secret
// that gates enrollment, the identity it asks to be issued, and the CSR proving
// possession of the key that identity will be bound to.
type EnrollParams struct {
	Token string
	Name  Name
	CSR   []byte
}

// EnrollResult is what the trust anchor returns on a successful enrollment: the
// signed leaf, the root to trust, and when the leaf expires so the node knows
// when to rotate.
type EnrollResult struct {
	LeafDER  []byte
	RootDER  []byte
	NotAfter time.Time
}

// Issuer is the trust anchor's issuance face: it validates an enrollment and
// signs a short-lived identity for the requesting node. The registry owns the
// concrete CA-backed implementation; this interface is what the enrollment
// endpoint serves, so the network adapter never holds the root key directly.
type Issuer interface {
	Issue(ctx context.Context, p EnrollParams) (EnrollResult, error)
}
