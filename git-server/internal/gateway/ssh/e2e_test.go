package ssh_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
	sshgw "github.com/nedanwr/conductor/git-server/internal/gateway/ssh"
	"github.com/nedanwr/conductor/git-server/internal/git"
	"github.com/nedanwr/conductor/git-server/internal/repostorage"
)

type resolver struct{ id uuid.UUID }

func (r resolver) Resolve(context.Context, string, string) (uuid.UUID, error) { return r.id, nil }

type allowAll struct{}

func (allowAll) Can(context.Context, auth.User, uuid.UUID, gitreq.Operation) (gitreq.Grant, error) {
	return gitreq.Grant{Level: gitreq.GrantLevelWrite}, nil
}

type oneNode struct{}

func (oneNode) ResolvePlacement(context.Context, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}
func (oneNode) CreatePlacement(context.Context, string, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}

// keyAuth authorizes exactly one client key.
type keyAuth struct {
	fingerprint string
	user        auth.User
}

func (k keyAuth) FromSSHKey(_ context.Context, key gossh.PublicKey) (auth.User, error) {
	if gossh.FingerprintSHA256(key) == k.fingerprint {
		return k.user, nil
	}
	return auth.User{}, fmt.Errorf("unknown key")
}

// TestCloneAndPushOverSSH drives a real git+ssh client through the SSH terminator
// against the real RepoStorage store: clone, push, and a re-clone that observes
// the pushed commit, proving the stateful stdio bridge works end to end.
func TestCloneAndPushOverSSH(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not on PATH")
	}

	root := t.TempDir()
	store := repostorage.NewStore(root, git.NewRunnerWithBin(gitBin))
	repoID := uuid.New()
	if err := store.CreateRepo(context.Background(), repoID.String(), "main"); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	hostSigner := newSigner(t)
	clientSigner, clientKeyPath := newClientKey(t)
	authn := keyAuth{
		fingerprint: gossh.FingerprintSHA256(clientSigner.PublicKey()),
		user:        auth.User{ID: uuid.New(), Username: "alice"},
	}

	gw := gateway.New(resolver{id: repoID}, allowAll{}, oneNode{}, gateway.NewSingleRouter("node-1", store))
	srv := sshgw.NewServer(gw, authn, hostSigner)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go srv.Serve(l)

	_, port, _ := net.SplitHostPort(l.Addr().String())
	repoURL := fmt.Sprintf("ssh://git@127.0.0.1:%s/alice/proj.git", port)
	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes", clientKeyPath)

	work := t.TempDir()
	clone := filepath.Join(work, "clone")
	runGit(t, work, sshCmd, "clone", repoURL, clone)

	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello ssh"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, sshCmd, "add", "README.md")
	runGit(t, clone, sshCmd, "-c", "user.email=a@b.c", "-c", "user.name=A", "commit", "-m", "init")
	runGit(t, clone, sshCmd, "push", "origin", "HEAD:refs/heads/main")

	fresh := filepath.Join(work, "fresh")
	runGit(t, work, sshCmd, "clone", repoURL, fresh)
	if got := runGit(t, fresh, sshCmd, "log", "-1", "--pretty=%s"); got != "init" {
		t.Fatalf("re-clone missing pushed commit: subject = %q", got)
	}
}

func newSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// newClientKey generates a client key, writes it in OpenSSH format to a temp
// file, and returns the signer and the file path for GIT_SSH_COMMAND.
func newClientKey(t *testing.T) (gossh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, path
}

func runGit(t *testing.T, dir, sshCmd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"GIT_SSH_COMMAND=" + sshCmd,
		"GIT_PROTOCOL=version=2",
		"GIT_TERMINAL_PROMPT=0",
		"HOME=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
