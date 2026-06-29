package service

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/enroll/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/enroll/v1/enrollv1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// Enroll obtains working identity for a node: it generates a fresh key and CSR,
// presents them with the bootstrap token to the enrollment endpoint at baseURL,
// and assembles the returned leaf and root into Material. The private key is
// created here and never leaves the process — only the CSR crosses the wire — so
// the node alone can wield the identity the trust anchor signs for it.
//
// httpClient dials the bootstrap endpoint in cleartext (the node has no identity
// to present yet); every connection the node makes thereafter uses the returned
// Material for mutual authentication.
func Enroll(ctx context.Context, httpClient connect.HTTPClient, baseURL, token string, name Name) (*Material, error) {
	key, csr, err := NewKeyAndCSR(name)
	if err != nil {
		return nil, err
	}
	client := enrollv1connect.NewEnrollServiceClient(httpClient, baseURL)
	resp, err := client.Enroll(ctx, connect.NewRequest(&v1.EnrollRequest{
		BootstrapToken: token,
		Role:           name.Role,
		NodeId:         name.NodeID,
		Csr:            csr,
	}))
	if err != nil {
		return nil, fmt.Errorf("service identity: enroll %s: %w", name, transport.FromConnectError(err))
	}
	return NewMaterial(key, resp.Msg.GetCertificate(), resp.Msg.GetCaBundle())
}
