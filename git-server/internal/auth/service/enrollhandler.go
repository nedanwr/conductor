package service

import (
	"context"

	"connectrpc.com/connect"

	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/enroll/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/enroll/v1/enrollv1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// EnrollHandler adapts an Issuer onto the generated EnrollService so the trust
// anchor's issuance can be served over the network. It runs on a bootstrap
// endpoint without mTLS — a joining node has no identity yet — and gates access
// through the bootstrap token the Issuer checks, mapping typed errors to Connect
// codes at the boundary.
type EnrollHandler struct {
	enrollv1connect.UnimplementedEnrollServiceHandler
	issuer Issuer
}

// NewEnrollHandler wraps issuer for serving.
func NewEnrollHandler(issuer Issuer) *EnrollHandler {
	return &EnrollHandler{issuer: issuer}
}

// Enroll serves the unary enrollment RPC: it forwards the request to the issuer
// and returns the signed identity, or a typed error a bad token or malformed CSR
// produced.
func (h *EnrollHandler) Enroll(ctx context.Context, req *connect.Request[v1.EnrollRequest]) (*connect.Response[v1.EnrollResponse], error) {
	res, err := h.issuer.Issue(ctx, EnrollParams{
		Token: req.Msg.GetBootstrapToken(),
		Name:  Name{Role: req.Msg.GetRole(), NodeID: req.Msg.GetNodeId()},
		CSR:   req.Msg.GetCsr(),
	})
	if err != nil {
		return nil, transport.AsConnectError(err)
	}
	return connect.NewResponse(&v1.EnrollResponse{
		Certificate:  res.LeafDER,
		CaBundle:     res.RootDER,
		NotAfterUnix: res.NotAfter.Unix(),
	}), nil
}
