package repostorage

import (
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1"
)

// toProtoGitRequest serializes a core GitRequest onto the wire type for the
// Connect boundary. It is the inverse of fromProtoGitRequest; the two keep the
// in-process and remote paths carrying identical payloads.
func toProtoGitRequest(req gitreq.GitRequest) *v1.GitRequest {
	return &v1.GitRequest{
		RepoId:    req.RepoID,
		Operation: toProtoOperation(req.Operation),
		Principal: &v1.Principal{
			UserId:    req.Principal.UserID,
			Anonymous: req.Principal.Anonymous,
		},
		Grant:         &v1.Grant{Level: toProtoGrantLevel(req.Grant.Level)},
		Protocol:      &v1.ProtocolParams{Version: int32(req.Protocol.Version), Capabilities: req.Protocol.Capabilities},
		CorrelationId: req.CorrelationID,
	}
}

// fromProtoGitRequest deserializes a wire GitRequest back into the core type.
func fromProtoGitRequest(p *v1.GitRequest) gitreq.GitRequest {
	if p == nil {
		return gitreq.GitRequest{}
	}
	req := gitreq.GitRequest{
		RepoID:        p.GetRepoId(),
		Operation:     fromProtoOperation(p.GetOperation()),
		Grant:         gitreq.Grant{Level: fromProtoGrantLevel(p.GetGrant().GetLevel())},
		CorrelationID: p.GetCorrelationId(),
	}
	if pr := p.GetPrincipal(); pr != nil {
		req.Principal = gitreq.Principal{UserID: pr.GetUserId(), Anonymous: pr.GetAnonymous()}
	}
	if pp := p.GetProtocol(); pp != nil {
		req.Protocol = gitreq.ProtocolParams{Version: int(pp.GetVersion()), Capabilities: pp.GetCapabilities()}
	}
	return req
}

func toProtoOperation(op gitreq.Operation) v1.Operation {
	switch op {
	case gitreq.OperationFetch:
		return v1.Operation_OPERATION_FETCH
	case gitreq.OperationReceive:
		return v1.Operation_OPERATION_RECEIVE
	default:
		return v1.Operation_OPERATION_UNSPECIFIED
	}
}

func fromProtoOperation(op v1.Operation) gitreq.Operation {
	switch op {
	case v1.Operation_OPERATION_FETCH:
		return gitreq.OperationFetch
	case v1.Operation_OPERATION_RECEIVE:
		return gitreq.OperationReceive
	default:
		return gitreq.OperationUnspecified
	}
}

func toProtoGrantLevel(l gitreq.GrantLevel) v1.GrantLevel {
	switch l {
	case gitreq.GrantLevelRead:
		return v1.GrantLevel_GRANT_LEVEL_READ
	case gitreq.GrantLevelWrite:
		return v1.GrantLevel_GRANT_LEVEL_WRITE
	case gitreq.GrantLevelAdmin:
		return v1.GrantLevel_GRANT_LEVEL_ADMIN
	default:
		return v1.GrantLevel_GRANT_LEVEL_UNSPECIFIED
	}
}

func fromProtoGrantLevel(l v1.GrantLevel) gitreq.GrantLevel {
	switch l {
	case v1.GrantLevel_GRANT_LEVEL_READ:
		return gitreq.GrantLevelRead
	case v1.GrantLevel_GRANT_LEVEL_WRITE:
		return gitreq.GrantLevelWrite
	case v1.GrantLevel_GRANT_LEVEL_ADMIN:
		return gitreq.GrantLevelAdmin
	default:
		return gitreq.GrantLevelUnspecified
	}
}
