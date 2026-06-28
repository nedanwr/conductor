package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1/registryv1connect"
)

// fakeQuerier is an in-memory stand-in for the generated placement queries so the
// registry can be exercised without standing up Postgres.
type fakeQuerier struct {
	rows map[uuid.UUID]queries.RepoPlacement
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{rows: make(map[uuid.UUID]queries.RepoPlacement)}
}

func (f *fakeQuerier) ResolvePlacement(_ context.Context, repoID uuid.UUID) (queries.RepoPlacement, error) {
	row, ok := f.rows[repoID]
	if !ok {
		return queries.RepoPlacement{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQuerier) CreatePlacement(_ context.Context, arg queries.CreatePlacementParams) (queries.RepoPlacement, error) {
	row := queries.RepoPlacement{RepoID: arg.RepoID, StorageNodeID: arg.StorageNodeID}
	f.rows[arg.RepoID] = row
	return row, nil
}

// localNode is the single node every repo resolves to in --mode=all.
var localNode = registry.Node{ID: "node-local", Address: "127.0.0.1:9000"}

func newRegistry() *Registry {
	return New(NewDirectory(newFakeQuerier()), NewMembership(localNode))
}

// TestCreateResolveRoundTrip proves a placement created for a repo resolves back
// to the same node.
func TestCreateResolveRoundTrip(t *testing.T) {
	r := newRegistry()
	repoID := uuid.NewString()

	created, err := r.CreatePlacement(context.Background(), repoID, localNode.ID)
	if err != nil {
		t.Fatalf("create placement: %v", err)
	}
	if created != localNode {
		t.Fatalf("created node = %+v, want %+v", created, localNode)
	}

	resolved, err := r.ResolvePlacement(context.Background(), repoID)
	if err != nil {
		t.Fatalf("resolve placement: %v", err)
	}
	if resolved != localNode {
		t.Fatalf("resolved node = %+v, want %+v", resolved, localNode)
	}
}

// TestSingleNodePlacement proves that in --mode=all every repo placed with an
// empty node id lands on — and resolves to — the single local node.
func TestSingleNodePlacement(t *testing.T) {
	r := newRegistry()

	for i := 0; i < 3; i++ {
		repoID := uuid.NewString()
		if _, err := r.CreatePlacement(context.Background(), repoID, ""); err != nil {
			t.Fatalf("create placement: %v", err)
		}
		node, err := r.ResolvePlacement(context.Background(), repoID)
		if err != nil {
			t.Fatalf("resolve placement: %v", err)
		}
		if node != localNode {
			t.Fatalf("repo %d resolved to %+v, want the single node %+v", i, node, localNode)
		}
	}
}

// TestResolveMissing proves an unplaced repo is a typed PlacementMiss.
func TestResolveMissing(t *testing.T) {
	r := newRegistry()
	_, err := r.ResolvePlacement(context.Background(), uuid.NewString())
	if giterr.KindOf(err) != giterr.KindPlacementMiss {
		t.Fatalf("kind = %v, want PlacementMiss", giterr.KindOf(err))
	}
}

// TestLocationTransparency proves the seam: the same requests against the
// in-process Registry and against its Connect client over loopback yield
// identical results, so the multi-binary split is a validation, not a behavior
// change.
func TestLocationTransparency(t *testing.T) {
	impl := newRegistry()

	path, handler := registryv1connect.NewRegistryServiceHandler(NewConnectHandler(impl))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewConnectClient(srv.Client(), srv.URL)

	repoID := uuid.NewString()

	localCreate, err := impl.CreatePlacement(context.Background(), repoID, localNode.ID)
	if err != nil {
		t.Fatalf("in-process create: %v", err)
	}
	remoteResolve, err := client.ResolvePlacement(context.Background(), repoID)
	if err != nil {
		t.Fatalf("remote resolve: %v", err)
	}
	if localCreate != remoteResolve {
		t.Fatalf("in-process create %+v != remote resolve %+v", localCreate, remoteResolve)
	}

	// A miss surfaces as the same typed Kind through the remote path.
	_, err = client.ResolvePlacement(context.Background(), uuid.NewString())
	if giterr.KindOf(err) != giterr.KindPlacementMiss {
		t.Fatalf("remote miss kind = %v, want PlacementMiss", giterr.KindOf(err))
	}
}
