package service

import (
	"context"
	"testing"
)

// TestNoopAnchorAdmitsAll confirms the --mode=all seam trivially verifies any
// caller. It exists so the seam is wired and the call site is exercised before
// a real anchor replaces it.
func TestNoopAnchorAdmitsAll(t *testing.T) {
	if _, err := NewAnchor().Verify(context.Background()); err != nil {
		t.Fatalf("noop anchor Verify = %v, want nil", err)
	}
}
