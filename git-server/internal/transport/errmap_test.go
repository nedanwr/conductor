package transport

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// TestConnectCodeFor covers every typed variant.
func TestConnectCodeFor(t *testing.T) {
	cases := []struct {
		kind giterr.Kind
		want connect.Code
	}{
		{giterr.KindUnauthorized, connect.CodePermissionDenied},
		{giterr.KindRepoNotFound, connect.CodeNotFound},
		{giterr.KindRefRejected, connect.CodeFailedPrecondition},
		{giterr.KindPlacementMiss, connect.CodeUnavailable},
		{giterr.KindGitExec, connect.CodeInternal},
		{giterr.KindDB, connect.CodeInternal},
		{giterr.KindUnknown, connect.CodeUnknown},
	}
	for _, c := range cases {
		if got := ConnectCodeFor(c.kind); got != c.want {
			t.Errorf("ConnectCodeFor(%v) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// TestAsConnectError maps typed errors, including wrapped ones, and passes nil
// through.
func TestAsConnectError(t *testing.T) {
	if AsConnectError(nil) != nil {
		t.Fatal("AsConnectError(nil) should be nil")
	}

	// An untyped error maps to unknown.
	if got := connect.CodeOf(AsConnectError(errors.New("boom"))); got != connect.CodeUnknown {
		t.Errorf("untyped error code = %v, want unknown", got)
	}

	// A typed error maps to its code even when wrapped in an outer error.
	wrapped := fmt.Errorf("outer: %w", giterr.RepoNotFound("repo %s", "x"))
	if got := connect.CodeOf(AsConnectError(wrapped)); got != connect.CodeNotFound {
		t.Errorf("wrapped RepoNotFound code = %v, want not_found", got)
	}
}

// TestKindOf confirms classification through the wrap chain.
func TestKindOf(t *testing.T) {
	err := fmt.Errorf("ctx: %w", giterr.Unauthorized("denied"))
	if got := giterr.KindOf(err); got != giterr.KindUnauthorized {
		t.Errorf("KindOf = %v, want Unauthorized", got)
	}
	if got := giterr.KindOf(errors.New("plain")); got != giterr.KindUnknown {
		t.Errorf("KindOf(plain) = %v, want Unknown", got)
	}
}
