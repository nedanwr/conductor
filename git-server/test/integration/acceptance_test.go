package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/app"
)

// TestSliceAcceptance proves the whole definition of done as one pass against a
// single co-located process: it boots --mode=all on Postgres, provisions a repo,
// a user, an SSH key, a token, and a grant entirely through the admin verbs, then
// drives a real git client through both transports — clone and push over HTTPS
// and over SSH — and confirms a write the principal is not entitled to is refused
// on both. The same artifact, the same wiring root, the same auth source of truth
// back every step; nothing here knows which transport carried a request.
func TestSliceAcceptance(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping end-to-end acceptance")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not on PATH")
	}

	// One storage root and database back both the running process and the admin
	// verbs, so provisioning and serving see the same world.
	root := t.TempDir()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("GITSERVER_STORAGE_ROOT", root)

	cfg := app.LoadConfig(app.ModeAll)
	cfg.HTTPSAddr = freeAddr(t)
	cfg.SSHAddr = freeAddr(t)

	// (1) --mode=all boots and connects Postgres.
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx, cfg) }()
	waitListening(t, cfg.HTTPSAddr)
	waitListening(t, cfg.SSHAddr)

	// (2) admin provisions repo + user + key + token + grant. A read-only second
	// user lets us drive the unauthorized-push case on both transports.
	suffix := unique()
	owner := "team_" + suffix
	repoAddr := owner + "/proj"
	alice := "alice_" + suffix
	bob := "bob_" + suffix

	runAdmin(t, "user", "create", alice)
	runAdmin(t, "user", "create", bob)

	aliceSigner, aliceKeyPath := newClientKey(t)
	bobSigner, bobKeyPath := newClientKey(t)
	runAdmin(t, "key", "add", alice, authorizedKeyFile(t, aliceSigner))
	runAdmin(t, "key", "add", bob, authorizedKeyFile(t, bobSigner))

	aliceToken := tokenFromAdmin(t, alice)
	bobToken := tokenFromAdmin(t, bob)

	runAdmin(t, "repo", "create", repoAddr, "--visibility", "private")
	runAdmin(t, "grant", alice, repoAddr, "write")
	runAdmin(t, "grant", bob, repoAddr, "read")

	sshHost, sshPort, _ := net.SplitHostPort(cfg.SSHAddr)
	sshBase := fmt.Sprintf("ssh://git@%s:%s/%s.git", sshHost, sshPort, repoAddr)

	httpsEnv := []string{
		"GIT_PROTOCOL=version=2",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ASKPASS=true",
	}
	httpsURL := func(user, token string) string {
		return "http://" + user + ":" + token + "@" + cfg.HTTPSAddr + "/" + repoAddr + ".git"
	}
	sshEnv := func(keyPath string) []string {
		return []string{
			"GIT_SSH_COMMAND=ssh -i " + keyPath + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes",
			"GIT_PROTOCOL=version=2",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_CONFIG_NOSYSTEM=1",
		}
	}

	// (3)+(4) clone and push succeed over HTTPS, then over SSH. The two transports
	// push to separate branches so they share the one repo without colliding.
	t.Run("https clone and push", func(t *testing.T) {
		cloneAndPush(t, httpsEnv, httpsURL(alice, aliceToken), "https")
	})
	t.Run("ssh clone and push", func(t *testing.T) {
		cloneAndPush(t, sshEnv(aliceKeyPath), sshBase, "ssh")
	})

	// (5) an unauthorized push is rejected with a typed error on both transports.
	// Bob holds read but not write, so receive-pack must be refused.
	t.Run("https unauthorized push rejected", func(t *testing.T) {
		assertPushRejected(t, httpsEnv, httpsURL(bob, bobToken))
	})
	t.Run("ssh unauthorized push rejected", func(t *testing.T) {
		assertPushRejected(t, sshEnv(bobKeyPath), sshBase)
	})

	// Graceful shutdown releases the listeners and the Postgres pool.
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not shut down within deadline")
	}
}

// cloneAndPush clones the empty repo, commits, pushes a branch named for the
// transport, then re-clones that branch and confirms the pushed commit arrived —
// proving the round trip end to end.
func cloneAndPush(t *testing.T, env []string, url, label string) {
	t.Helper()
	work := t.TempDir()
	clone := filepath.Join(work, "clone")
	runGit(t, work, env, "clone", url, clone)

	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello "+label), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, env, "add", "README.md")
	runGit(t, clone, env, "-c", "user.email=a@b.c", "-c", "user.name=A", "commit", "-m", "init "+label)
	runGit(t, clone, env, "push", "origin", "HEAD:refs/heads/"+label)

	fresh := filepath.Join(work, "fresh")
	runGit(t, work, env, "clone", "--branch", label, url, fresh)
	if got := runGit(t, fresh, env, "log", "-1", "--pretty=%s"); got != "init "+label {
		t.Fatalf("re-clone over %s missing pushed commit: subject = %q", label, got)
	}
}

// assertPushRejected confirms a principal without write cannot push: the clone
// (read) succeeds, but the push is refused and git exits non-zero.
func assertPushRejected(t *testing.T, env []string, url string) {
	t.Helper()
	work := t.TempDir()
	clone := filepath.Join(work, "clone")
	runGit(t, work, env, "clone", url, clone)

	if err := os.WriteFile(filepath.Join(clone, "intruder.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, env, "add", "intruder.txt")
	runGit(t, clone, env, "-c", "user.email=b@b.c", "-c", "user.name=B", "commit", "-m", "intrude")

	out, err := tryGit(clone, env, "push", "origin", "HEAD:refs/heads/intrusion")
	if err == nil {
		t.Fatalf("unauthorized push unexpectedly succeeded:\n%s", out)
	}
}

// runGit runs a git command and fails the test on error.
func runGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := tryGit(dir, env, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

// tryGit runs a git command and returns its combined output and error without
// failing, for cases that expect a non-zero exit.
func tryGit(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"HOME=" + dir}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runAdmin runs a provisioning verb and fails the test on error.
func runAdmin(t *testing.T, args ...string) {
	t.Helper()
	if _, err := captureAdmin(t, args...); err != nil {
		t.Fatalf("admin %v: %v", args, err)
	}
}

// tokenFromAdmin mints a token for a user and extracts the raw value the verb
// prints once.
func tokenFromAdmin(t *testing.T, username string) string {
	t.Helper()
	out, err := captureAdmin(t, "token", "create", username)
	if err != nil {
		t.Fatalf("admin token create %s: %v", username, err)
	}
	_, raw, ok := strings.Cut(out, "): ")
	if !ok {
		t.Fatalf("could not parse token from admin output: %q", out)
	}
	return strings.TrimSpace(raw)
}

// captureAdmin runs an admin verb while capturing what it writes to stdout, so a
// test can read back the values (such as a freshly minted token) the verb prints.
func captureAdmin(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := app.Admin(context.Background(), args)
	os.Stdout = orig
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()
	return string(out), runErr
}

// newClientKey generates an ed25519 client key, writes the private half in
// OpenSSH format for GIT_SSH_COMMAND, and returns the signer and that path.
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

// authorizedKeyFile writes a signer's public half in authorized_keys form and
// returns the path, ready to hand to `admin key add`.
func authorizedKeyFile(t *testing.T, signer gossh.Signer) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(path, gossh.MarshalAuthorizedKey(signer.PublicKey()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// freeAddr reserves a free loopback port and returns it as host:port. The
// listener is closed immediately; the small reuse window is acceptable for tests.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// waitListening blocks until addr accepts a connection or the deadline passes.
func waitListening(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s", addr)
}

var counter atomic.Uint64

// unique returns a process-unique suffix so concurrent runs against a shared
// database never collide on owner, user, or repo names.
func unique() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + strconv.FormatUint(counter.Add(1), 36)
}
