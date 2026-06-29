package ssh

import (
	"testing"

	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
)

func TestIntakeForUploadPack(t *testing.T) {
	principal := auth.User{ID: uuid.New(), Username: "alice"}
	in, err := intakeFor("git-upload-pack 'alice/proj.git'", principal, "version=2")
	if err != nil {
		t.Fatalf("intakeFor: %v", err)
	}
	if in.Owner != "alice" || in.Repo != "proj" {
		t.Errorf("owner/repo = %q/%q, want alice/proj", in.Owner, in.Repo)
	}
	if in.Operation != gitreq.OperationFetch {
		t.Errorf("op = %v, want fetch", in.Operation)
	}
	if in.Protocol.Version != 2 {
		t.Errorf("version = %d, want 2", in.Protocol.Version)
	}
	if in.Protocol.Stateless || in.Protocol.AdvertiseRefs {
		t.Errorf("ssh exchange must be stateful: %+v", in.Protocol)
	}
	if in.Principal != principal {
		t.Errorf("principal = %+v, want %+v", in.Principal, principal)
	}
}

func TestIntakeForReceivePack(t *testing.T) {
	in, err := intakeFor(`git-receive-pack "/bob/site.git"`, auth.User{}, "")
	if err != nil {
		t.Fatalf("intakeFor: %v", err)
	}
	if in.Owner != "bob" || in.Repo != "site" {
		t.Errorf("owner/repo = %q/%q, want bob/site", in.Owner, in.Repo)
	}
	if in.Operation != gitreq.OperationReceive {
		t.Errorf("op = %v, want receive", in.Operation)
	}
	if in.Protocol.Version != 0 {
		t.Errorf("version = %d, want 0 fallback", in.Protocol.Version)
	}
}

func TestIntakeForRejectsBadCommand(t *testing.T) {
	for _, cmd := range []string{
		"git-upload-pack",         // no repo arg
		"scp -t /etc/passwd",      // not a git service
		"git-upload-pack 'a/b/c'", // nested path
		"git-upload-pack ''",      // empty path
	} {
		if _, err := intakeFor(cmd, auth.User{}, ""); err == nil {
			t.Errorf("intakeFor(%q) accepted a bad command", cmd)
		} else if k := giterr.KindOf(err); k != giterr.KindUnauthorized && k != giterr.KindRepoNotFound {
			t.Errorf("intakeFor(%q) err kind = %v, want typed", cmd, k)
		}
	}
}

func TestParseExecRoundTrip(t *testing.T) {
	payload := gossh.Marshal(struct{ Command string }{"git-upload-pack 'a/b.git'"})
	cmd, ok := parseExec(payload)
	if !ok || cmd != "git-upload-pack 'a/b.git'" {
		t.Fatalf("parseExec = (%q,%v)", cmd, ok)
	}
}

func TestParseEnvRoundTrip(t *testing.T) {
	payload := gossh.Marshal(struct{ Name, Value string }{"GIT_PROTOCOL", "version=2"})
	name, value, ok := parseEnv(payload)
	if !ok || name != "GIT_PROTOCOL" || value != "version=2" {
		t.Fatalf("parseEnv = (%q,%q,%v)", name, value, ok)
	}
}

func TestPrincipalFrom(t *testing.T) {
	id := uuid.New()
	perms := &gossh.Permissions{Extensions: map[string]string{
		extUserID:   id.String(),
		extUsername: "alice",
	}}
	u := principalFrom(perms)
	if u.ID != id || u.Username != "alice" || u.Anonymous {
		t.Errorf("principalFrom = %+v, want id=%v alice", u, id)
	}

	if u := principalFrom(nil); !u.Anonymous {
		t.Errorf("nil perms should be anonymous, got %+v", u)
	}
}
