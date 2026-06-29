package registry

import (
	"context"
	"testing"
	"time"

	"github.com/nedanwr/conductor/git-server/internal/auth/service"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// TestTrustAnchorGatesOnBootstrapToken confirms the anchor issues an identity
// only to a caller presenting the configured bootstrap token, signs a leaf the
// requester can wield when it does, and refuses both wrong and empty tokens.
func TestTrustAnchorGatesOnBootstrapToken(t *testing.T) {
	ca, err := service.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	const token = "join-me"
	anchor := NewTrustAnchor(ca, token, time.Hour)

	name := service.Name{Role: "gateway", NodeID: "gw-9"}
	_, csr, err := service.NewKeyAndCSR(name)
	if err != nil {
		t.Fatal(err)
	}

	res, err := anchor.Issue(context.Background(), service.EnrollParams{Token: token, Name: name, CSR: csr})
	if err != nil {
		t.Fatalf("Issue with valid token: %v", err)
	}
	if mat, err := service.NewMaterial(nil, res.LeafDER, res.RootDER); err != nil {
		t.Fatalf("issued material invalid: %v", err)
	} else if mat.Name() != name {
		t.Fatalf("issued identity = %+v, want %+v", mat.Name(), name)
	}

	for _, bad := range []string{"nope", ""} {
		_, err := anchor.Issue(context.Background(), service.EnrollParams{Token: bad, Name: name, CSR: csr})
		if giterr.KindOf(err) != giterr.KindUnauthorized {
			t.Fatalf("token %q: kind = %v, want Unauthorized", bad, giterr.KindOf(err))
		}
	}
}

// TestTrustAnchorEmptyTokenRefusesAll confirms an anchor configured with no
// bootstrap token admits no one, rather than degrading to admitting everyone.
func TestTrustAnchorEmptyTokenRefusesAll(t *testing.T) {
	ca, err := service.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	anchor := NewTrustAnchor(ca, "", time.Hour)
	_, err = anchor.Issue(context.Background(), service.EnrollParams{Token: "", Name: service.Name{Role: "cache", NodeID: "c1"}})
	if giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("kind = %v, want Unauthorized", giterr.KindOf(err))
	}
}
