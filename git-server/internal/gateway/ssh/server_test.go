package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
)

type fakeResolver struct{ id uuid.UUID }

func (f fakeResolver) Resolve(context.Context, string, string) (uuid.UUID, error) {
	return f.id, nil
}

// fakeAuthorizer grants reads but denies pushes, so the tests cover both the
// allowed clone and the rejected push over SSH.
type fakeAuthorizer struct{}

func (fakeAuthorizer) Can(_ context.Context, _ auth.User, _ uuid.UUID, op gitreq.Operation) (gitreq.Grant, error) {
	if op == gitreq.OperationReceive {
		return gitreq.Grant{}, giterr.Unauthorized("push denied: write permission required")
	}
	return gitreq.Grant{Level: gitreq.GrantLevelRead}, nil
}

type fakeRegistry struct{}

func (fakeRegistry) ResolvePlacement(context.Context, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}
func (fakeRegistry) CreatePlacement(context.Context, string, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}

type echoStore struct{}

func (echoStore) CreateRepo(context.Context, string, string) error { return nil }
func (echoStore) Fetch(_ context.Context, _ gitreq.GitRequest, _ io.Reader, w io.Writer) error {
	_, err := io.WriteString(w, "PACK-DATA")
	return err
}
func (echoStore) Receive(context.Context, gitreq.GitRequest, io.Reader, io.Writer) error {
	return nil
}

// keyAuth resolves exactly one authorized public key to a user; any other key is
// an authentication failure.
type keyAuth struct {
	authorized string
	user       auth.User
}

func (k keyAuth) FromSSHKey(_ context.Context, key gossh.PublicKey) (auth.User, error) {
	if gossh.FingerprintSHA256(key) == k.authorized {
		return k.user, nil
	}
	return auth.User{}, giterr.Unauthorized("unknown ssh key")
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

// listen starts the SSH terminator on a loopback TCP port and returns its
// address. A real socket (not net.Pipe) is needed: the SSH version exchange
// writes before reading, which deadlocks on an unbuffered pipe.
func listen(t *testing.T, authz gateway.Authorizer, authn keyAuth) string {
	t.Helper()
	gw := gateway.New(fakeResolver{id: uuid.New()}, authz, fakeRegistry{}, gateway.NewSingleRouter("node-1", echoStore{}))
	srv := NewServer(gw, authn, newSigner(t))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go srv.Serve(l)
	return l.Addr().String()
}

// dialServer connects to the SSH terminator at addr and returns a ready client.
func dialServer(t *testing.T, addr string, clientSigner gossh.Signer) *gossh.Client {
	t.Helper()
	cfg := &gossh.ClientConfig{
		User:            "git",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(clientSigner)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	client, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestSSHCloneStreamsPack(t *testing.T) {
	signer := newSigner(t)
	authn := keyAuth{
		authorized: gossh.FingerprintSHA256(signer.PublicKey()),
		user:       auth.User{ID: uuid.New(), Username: "alice"},
	}
	client := dialServer(t, listen(t, fakeAuthorizer{}, authn), signer)

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_ = sess.Setenv("GIT_PROTOCOL", "version=2")

	out, err := sess.Output("git-upload-pack 'alice/proj.git'")
	if err != nil {
		t.Fatalf("upload-pack exec: %v", err)
	}
	if string(out) != "PACK-DATA" {
		t.Errorf("output = %q, want PACK-DATA", out)
	}
}

func TestSSHPushDeniedExitsNonZero(t *testing.T) {
	signer := newSigner(t)
	authn := keyAuth{
		authorized: gossh.FingerprintSHA256(signer.PublicKey()),
		user:       auth.User{ID: uuid.New(), Username: "alice"},
	}
	client := dialServer(t, listen(t, fakeAuthorizer{}, authn), signer)

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	var stderr strings.Builder
	sess.Stderr = &stderr
	err = sess.Run("git-receive-pack 'alice/proj.git'")
	if err == nil {
		t.Fatal("denied push exited zero")
	}
	if exitErr, ok := err.(*gossh.ExitError); !ok || exitErr.ExitStatus() == 0 {
		t.Errorf("err = %v, want non-zero exit status", err)
	}
	if !strings.Contains(stderr.String(), "denied") {
		t.Errorf("stderr = %q, want a denial message", stderr.String())
	}
}

func TestSSHUnknownKeyRejected(t *testing.T) {
	authorizedSigner := newSigner(t)
	authn := keyAuth{
		authorized: gossh.FingerprintSHA256(authorizedSigner.PublicKey()),
		user:       auth.User{ID: uuid.New(), Username: "alice"},
	}

	addr := listen(t, fakeAuthorizer{}, authn)

	stranger := newSigner(t) // not the authorized key
	cfg := &gossh.ClientConfig{
		User:            "git",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(stranger)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	if _, err := gossh.Dial("tcp", addr, cfg); err == nil {
		t.Fatal("handshake with unknown key succeeded")
	}
}
