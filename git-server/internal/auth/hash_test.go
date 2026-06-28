package auth

import "testing"

// TestHashTokenDeterministicAndOpaque confirms the hash is stable for a given
// token (so lookup works) and does not echo the raw token (so a leaked row does
// not reveal credentials).
func TestHashTokenDeterministicAndOpaque(t *testing.T) {
	raw := "super-secret-token"
	h1 := HashToken(raw)
	h2 := HashToken(raw)
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q != %q", h1, h2)
	}
	if h1 == raw || len(h1) != 64 {
		t.Fatalf("hash %q looks wrong (want 64-hex, opaque)", h1)
	}
	if HashToken("other") == h1 {
		t.Fatal("distinct tokens collided")
	}
}
