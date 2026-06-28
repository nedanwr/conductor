package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken derives the at-rest representation of an access token. Only the hash
// is ever stored or compared; the raw token never touches the database. The
// digest is deterministic so a presented token can be looked up directly.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
