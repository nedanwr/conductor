package ssh

import (
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
)

// parseExec decodes an SSH "exec" request payload into the command string.
func parseExec(payload []byte) (string, bool) {
	var msg struct{ Command string }
	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return "", false
	}
	return msg.Command, true
}

// parseEnv decodes an SSH "env" request payload into a name/value pair.
func parseEnv(payload []byte) (name, value string, ok bool) {
	var msg struct{ Name, Value string }
	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return "", "", false
	}
	return msg.Name, msg.Value, true
}

// intakeFor turns a git exec command (e.g. `git-upload-pack 'owner/repo.git'`)
// and the negotiated GIT_PROTOCOL into a transport-neutral intake. The exchange
// is stateful — an SSH channel is a persistent duplex stream — so neither the
// stateless nor advertise shape is set.
func intakeFor(cmd string, principal auth.User, protocol string) (gateway.Intake, error) {
	service, arg, found := strings.Cut(strings.TrimSpace(cmd), " ")
	if !found {
		return gateway.Intake{}, giterr.Unauthorized("unsupported command %q", cmd)
	}

	op, err := gateway.OperationForService(service)
	if err != nil {
		return gateway.Intake{}, err
	}

	owner, repo, err := parseRepoArg(arg)
	if err != nil {
		return gateway.Intake{}, err
	}

	return gateway.Intake{
		Owner:     owner,
		Repo:      repo,
		Operation: op,
		Principal: principal,
		Protocol:  gitreq.ProtocolParams{Version: protocolVersion(protocol)},
	}, nil
}

// parseRepoArg extracts owner/repo from a git command argument. Git quotes the
// path and may prefix a slash; the optional .git suffix is dropped. The result
// must be exactly owner/repo.
func parseRepoArg(arg string) (owner, repo string, err error) {
	path := strings.Trim(strings.TrimSpace(arg), "'\"")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	owner, repo, found := strings.Cut(path, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", giterr.RepoNotFound("malformed repo path %q", arg)
	}
	return owner, repo, nil
}

// protocolVersion reads the git protocol version from a GIT_PROTOCOL value,
// defaulting to 0 (the fallback) when v2 is not requested.
func protocolVersion(value string) int {
	for _, field := range strings.Split(value, ":") {
		if strings.TrimSpace(field) == "version=2" {
			return 2
		}
	}
	return 0
}

// uuidParse parses a stored user id, used when reconstructing the principal from
// the connection permissions.
func uuidParse(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
