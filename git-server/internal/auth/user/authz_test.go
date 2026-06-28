package user

import (
	"testing"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
)

// TestDecideTruthTable covers {none,read,write,admin} × {private,public} ×
// {fetch,receive}: the full authorization matrix, including the anonymous /
// no-permission case (LevelNone) and the typed-Unauthorized denials.
func TestDecideTruthTable(t *testing.T) {
	cases := []struct {
		level   auth.Level
		vis     auth.Visibility
		op      gitreq.Operation
		want    gitreq.GrantLevel
		allowed bool
	}{
		// Private fetch: needs read or above; no permission is denied.
		{auth.LevelNone, auth.VisibilityPrivate, gitreq.OperationFetch, gitreq.GrantLevelUnspecified, false},
		{auth.LevelRead, auth.VisibilityPrivate, gitreq.OperationFetch, gitreq.GrantLevelRead, true},
		{auth.LevelWrite, auth.VisibilityPrivate, gitreq.OperationFetch, gitreq.GrantLevelWrite, true},
		{auth.LevelAdmin, auth.VisibilityPrivate, gitreq.OperationFetch, gitreq.GrantLevelAdmin, true},

		// Public fetch: anyone may read, even with no permission (anonymous).
		{auth.LevelNone, auth.VisibilityPublic, gitreq.OperationFetch, gitreq.GrantLevelRead, true},
		{auth.LevelRead, auth.VisibilityPublic, gitreq.OperationFetch, gitreq.GrantLevelRead, true},
		{auth.LevelWrite, auth.VisibilityPublic, gitreq.OperationFetch, gitreq.GrantLevelWrite, true},
		{auth.LevelAdmin, auth.VisibilityPublic, gitreq.OperationFetch, gitreq.GrantLevelAdmin, true},

		// Private receive: needs write or above.
		{auth.LevelNone, auth.VisibilityPrivate, gitreq.OperationReceive, gitreq.GrantLevelUnspecified, false},
		{auth.LevelRead, auth.VisibilityPrivate, gitreq.OperationReceive, gitreq.GrantLevelUnspecified, false},
		{auth.LevelWrite, auth.VisibilityPrivate, gitreq.OperationReceive, gitreq.GrantLevelWrite, true},
		{auth.LevelAdmin, auth.VisibilityPrivate, gitreq.OperationReceive, gitreq.GrantLevelAdmin, true},

		// Public receive: visibility never relaxes write; read/none denied.
		{auth.LevelNone, auth.VisibilityPublic, gitreq.OperationReceive, gitreq.GrantLevelUnspecified, false},
		{auth.LevelRead, auth.VisibilityPublic, gitreq.OperationReceive, gitreq.GrantLevelUnspecified, false},
		{auth.LevelWrite, auth.VisibilityPublic, gitreq.OperationReceive, gitreq.GrantLevelWrite, true},
		{auth.LevelAdmin, auth.VisibilityPublic, gitreq.OperationReceive, gitreq.GrantLevelAdmin, true},
	}

	for _, c := range cases {
		grant, err := decide(c.op, c.vis, c.level)
		if c.allowed {
			if err != nil {
				t.Errorf("decide(%v,%v,%v) = error %v, want allow", c.op, c.vis, c.level, err)
				continue
			}
			if grant.Level != c.want {
				t.Errorf("decide(%v,%v,%v) grant = %v, want %v", c.op, c.vis, c.level, grant.Level, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("decide(%v,%v,%v) = allow, want deny", c.op, c.vis, c.level)
			continue
		}
		if got := giterr.KindOf(err); got != giterr.KindUnauthorized {
			t.Errorf("decide(%v,%v,%v) denial kind = %v, want Unauthorized", c.op, c.vis, c.level, got)
		}
	}
}

// TestDecideUnknownOperation rejects an unspecified operation as Unauthorized
// rather than silently granting.
func TestDecideUnknownOperation(t *testing.T) {
	_, err := decide(gitreq.OperationUnspecified, auth.VisibilityPublic, auth.LevelAdmin)
	if giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("unspecified op kind = %v, want Unauthorized", giterr.KindOf(err))
	}
}
