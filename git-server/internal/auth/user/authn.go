// Package user resolves user identity (SSH pubkey / HTTPS token → user) and
// holds Can(user, repo, action) — the sole authZ evaluator.
package user

import (
	"context"

	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
)

// Authenticator resolves a presented credential to a user. It never decides
// access — that is Authorizer's job.
type Authenticator struct {
	store *auth.Store
}

// NewAuthenticator builds an Authenticator over the auth store.
func NewAuthenticator(store *auth.Store) *Authenticator {
	return &Authenticator{store: store}
}

// FromSSHKey resolves the user owning an offered SSH public key, matching on its
// SHA256 fingerprint. Used in the SSH public-key authN callback.
func (a *Authenticator) FromSSHKey(ctx context.Context, key ssh.PublicKey) (auth.User, error) {
	return a.store.UserByFingerprint(ctx, ssh.FingerprintSHA256(key))
}

// FromToken resolves the user presenting an HTTPS access token. The raw token is
// hashed before lookup; it is never stored or compared in the clear.
func (a *Authenticator) FromToken(ctx context.Context, raw string) (auth.User, error) {
	return a.store.UserByTokenHash(ctx, auth.HashToken(raw))
}
