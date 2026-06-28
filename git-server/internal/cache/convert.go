package cache

import (
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	repov1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1"
)

// The cache reuses the repostorage GitRequest boundary type on the wire so the
// passthrough is byte-exact. These converters keep the in-process and remote
// read paths carrying identical payloads, mirroring the repostorage seam.

// toProtoGitRequest serializes a core GitRequest onto the wire type.
func toProtoGitRequest(req gitreq.GitRequest) *repov1.GitRequest {
	return &repov1.GitRequest{
		RepoId:    req.RepoID,
		Operation: toProtoOperation(req.Operation),
		Principal: &repov1.Principal{
			UserId:    req.Principal.UserID,
			Anonymous: req.Principal.Anonymous,
		},
		Grant:         &repov1.Grant{Level: toProtoGrantLevel(req.Grant.Level)},
		Protocol:      &repov1.ProtocolParams{Version: int32(req.Protocol.Version), Capabilities: req.Protocol.Capabilities},
		CorrelationId: req.CorrelationID,
	}
}

// fromProtoGitRequest deserializes a wire GitRequest back into the core type.
func fromProtoGitRequest(p *repov1.GitRequest) gitreq.GitRequest {
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

func toProtoOperation(op gitreq.Operation) repov1.Operation {
	switch op {
	case gitreq.OperationFetch:
		return repov1.Operation_OPERATION_FETCH
	case gitreq.OperationReceive:
		return repov1.Operation_OPERATION_RECEIVE
	default:
		return repov1.Operation_OPERATION_UNSPECIFIED
	}
}

func fromProtoOperation(op repov1.Operation) gitreq.Operation {
	switch op {
	case repov1.Operation_OPERATION_FETCH:
		return gitreq.OperationFetch
	case repov1.Operation_OPERATION_RECEIVE:
		return gitreq.OperationReceive
	default:
		return gitreq.OperationUnspecified
	}
}

func toProtoGrantLevel(l gitreq.GrantLevel) repov1.GrantLevel {
	switch l {
	case gitreq.GrantLevelRead:
		return repov1.GrantLevel_GRANT_LEVEL_READ
	case gitreq.GrantLevelWrite:
		return repov1.GrantLevel_GRANT_LEVEL_WRITE
	case gitreq.GrantLevelAdmin:
		return repov1.GrantLevel_GRANT_LEVEL_ADMIN
	default:
		return repov1.GrantLevel_GRANT_LEVEL_UNSPECIFIED
	}
}

func fromProtoGrantLevel(l repov1.GrantLevel) gitreq.GrantLevel {
	switch l {
	case repov1.GrantLevel_GRANT_LEVEL_READ:
		return gitreq.GrantLevelRead
	case repov1.GrantLevel_GRANT_LEVEL_WRITE:
		return gitreq.GrantLevelWrite
	case repov1.GrantLevel_GRANT_LEVEL_ADMIN:
		return gitreq.GrantLevelAdmin
	default:
		return gitreq.GrantLevelUnspecified
	}
}
