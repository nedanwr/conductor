package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/nedanwr/conductor/git-server/internal/app"
)

// TestSplitDeployment proves the same artifact serves clone and push when its
// services run as separate processes wired over loopback Connect, with results
// identical to the co-located case. The registry, the repo-storage node, and the
// edge gateway each boot from their own configuration into a distinct role; the
// gateway reaches its peers as remote Connect endpoints rather than by in-process
// call. Provisioning, the auth source of truth, and the on-disk repositories are
// shared with those services exactly as in the single-process deployment, so the
// only thing that changes between the two topologies is how the edge reaches
// storage and placement — by network here, by method call there. Nothing in any
// service knows which binding it received.
func TestSplitDeployment(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping split-deployment acceptance")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not on PATH")
	}

	// One storage root, one database, one node id back the admin verbs, the
	// storage process, and the gateway alike — so provisioning places a repo on
	// the same node the edge routes to, and writes land on the same disk the
	// storage process serves.
	const nodeID = "node-split"
	root := t.TempDir()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("GITSERVER_STORAGE_ROOT", root)
	t.Setenv("GITSERVER_NODE_ID", nodeID)

	// The two internal services listen on loopback Connect addresses the gateway
	// will dial; the edge listens on the git transports the client drives.
	registryAddr := freeAddr(t)
	storageAddr := freeAddr(t)

	registryCfg := app.LoadConfig(app.ModeRegistry)
	registryCfg.ConnectAddr = registryAddr

	storageCfg := app.LoadConfig(app.ModeRepoStorage)
	storageCfg.ConnectAddr = storageAddr

	gatewayCfg := app.LoadConfig(app.ModeGateway)
	gatewayCfg.HTTPSAddr = freeAddr(t)
	gatewayCfg.SSHAddr = freeAddr(t)
	gatewayCfg.RegistryURL = "http://" + registryAddr
	gatewayCfg.RepoStorageURL = "http://" + storageAddr

	// Boot each role as its own process over loopback. Storage and registry come
	// up first so the edge's peers are reachable when it starts serving.
	ctx, cancel := context.WithCancel(context.Background())
	fleet := startFleet(t, ctx, registryCfg, storageCfg, gatewayCfg)
	waitListening(t, registryAddr)
	waitListening(t, storageAddr)
	waitListening(t, gatewayCfg.HTTPSAddr)
	waitListening(t, gatewayCfg.SSHAddr)

	// Provision through the admin verbs, exactly as the co-located case does. The
	// repo is placed on the split node id so the edge's router accepts it.
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

	sshHost, sshPort, _ := net.SplitHostPort(gatewayCfg.SSHAddr)
	sshBase := fmt.Sprintf("ssh://git@%s:%s/%s.git", sshHost, sshPort, repoAddr)

	httpsEnv := []string{
		"GIT_PROTOCOL=version=2",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ASKPASS=true",
	}
	httpsURL := func(user, token string) string {
		return "http://" + user + ":" + token + "@" + gatewayCfg.HTTPSAddr + "/" + repoAddr + ".git"
	}
	sshEnv := func(keyPath string) []string {
		return []string{
			"GIT_SSH_COMMAND=ssh -i " + keyPath + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes",
			"GIT_PROTOCOL=version=2",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_CONFIG_NOSYSTEM=1",
		}
	}

	// Clone and push succeed over both transports against the split fleet, every
	// byte of pack data crossing the network to the storage process and back.
	t.Run("https clone and push", func(t *testing.T) {
		cloneAndPush(t, httpsEnv, httpsURL(alice, aliceToken), "https")
	})
	t.Run("ssh clone and push", func(t *testing.T) {
		cloneAndPush(t, sshEnv(aliceKeyPath), sshBase, "ssh")
	})

	// AuthZ is enforced at the edge regardless of topology: Bob holds read but not
	// write, so his push is refused on both transports.
	t.Run("https unauthorized push rejected", func(t *testing.T) {
		assertPushRejected(t, httpsEnv, httpsURL(bob, bobToken))
	})
	t.Run("ssh unauthorized push rejected", func(t *testing.T) {
		assertPushRejected(t, sshEnv(bobKeyPath), sshBase)
	})

	// Tear the whole fleet down together and confirm every process shut down
	// cleanly.
	cancel()
	fleet.wait(t)
}

// fleet tracks the running processes of a split deployment so the test can wait
// for them all to exit on shutdown.
type fleet struct {
	errs []chan error
}

// startFleet launches one process per configuration, each driving the shared Run
// lifecycle under the common context, and returns a handle to await their exit.
func startFleet(t *testing.T, ctx context.Context, cfgs ...app.Config) *fleet {
	t.Helper()
	f := &fleet{}
	for _, cfg := range cfgs {
		cfg := cfg
		errCh := make(chan error, 1)
		f.errs = append(f.errs, errCh)
		go func() { errCh <- app.Run(ctx, cfg) }()
	}
	return f
}

// wait blocks until every process in the fleet returns, failing the test on any
// error or if a process does not stop within the deadline.
func (f *fleet) wait(t *testing.T) {
	t.Helper()
	for _, errCh := range f.errs {
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("process returned error: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("a process did not shut down within deadline")
		}
	}
}
