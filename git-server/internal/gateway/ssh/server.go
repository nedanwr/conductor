package ssh

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
)

// KeyAuthenticator resolves an offered SSH public key to a user. It is the SSH
// half of authN; the same user identity and authZ source of truth back every
// transport.
type KeyAuthenticator interface {
	FromSSHKey(ctx context.Context, key ssh.PublicKey) (auth.User, error)
}

// Server terminates SSH git connections. It owns the host key and the public-key
// handshake; once a channel is open it parses the git command and converges on
// the Gateway core, identically to the HTTPS edge.
type Server struct {
	gw     *gateway.Gateway
	authn  KeyAuthenticator
	config *ssh.ServerConfig
}

// NewServer builds an SSH terminator over the Gateway core, the key
// authenticator, and the host key presented to clients. Only public-key auth is
// offered; an unknown key is rejected at the handshake (there is no anonymous
// SSH — anonymous fetch is an HTTPS path).
func NewServer(gw *gateway.Gateway, authn KeyAuthenticator, hostKey ssh.Signer) *Server {
	s := &Server{gw: gw, authn: authn}
	cfg := &ssh.ServerConfig{PublicKeyCallback: s.authenticate}
	cfg.AddHostKey(hostKey)
	s.config = cfg
	return s
}

// authenticate is the public-key callback: it resolves the offered key to a user
// and records the identity in the connection permissions for the session to read
// back. An unknown key fails the handshake.
func (s *Server) authenticate(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	user, err := s.authn.FromSSHKey(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return &ssh.Permissions{Extensions: map[string]string{
		extUserID:   user.ID.String(),
		extUsername: user.Username,
	}}, nil
}

// Serve accepts connections on l until it is closed, handshaking and dispatching
// each in its own goroutine.
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

// handleConn completes the SSH handshake and serves the session channels on the
// connection.
func (s *Server) handleConn(nc net.Conn) {
	defer nc.Close()

	serverConn, chans, globalReqs, err := ssh.NewServerConn(nc, s.config)
	if err != nil {
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(globalReqs)

	principal := principalFrom(serverConn.Permissions)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		s.handleSession(newChan, principal)
	}
}

// handleSession serves one session channel: it collects the GIT_PROTOCOL env
// request, then runs the single exec command (the git pack request) and closes.
// Interactive requests (shell, pty) are rejected — this is a git endpoint, not a
// login shell.
func (s *Server) handleSession(newChan ssh.NewChannel, principal auth.User) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		return
	}
	defer ch.Close()

	var protocol string
	for req := range reqs {
		switch req.Type {
		case "env":
			if name, value, ok := parseEnv(req.Payload); ok && name == "GIT_PROTOCOL" {
				protocol = value
			}
			reply(req, true)
		case "exec":
			cmd, ok := parseExec(req.Payload)
			reply(req, ok)
			if !ok {
				writeFail(ch, giterr.Unauthorized("malformed exec request"))
				return
			}
			s.runExec(ch, principal, cmd, protocol)
			return
		default:
			reply(req, false)
		}
	}
}

// runExec parses the git command, builds the intake, and routes it through the
// Gateway, streaming over the channel. The exec channel is a persistent duplex
// stream, so the exchange is stateful. The git exit status is reported back so
// the client sees success or failure.
func (s *Server) runExec(ch ssh.Channel, principal auth.User, cmd, protocol string) {
	in, err := intakeFor(cmd, principal, protocol)
	if err != nil {
		writeFail(ch, err)
		return
	}

	if err := s.gw.Serve(context.Background(), in, ch, ch); err != nil {
		writeFail(ch, err)
		return
	}
	sendExit(ch, 0)
}

// writeFail reports a typed error to the client on the channel's stderr and exits
// non-zero, mirroring how a failed git command would terminate.
func writeFail(ch ssh.Channel, err error) {
	fmt.Fprintln(ch.Stderr(), "git-server:", err.Error())
	sendExit(ch, 1)
}

// sendExit sends the exec exit status and closes the write side.
func sendExit(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
	_ = ch.CloseWrite()
}

// reply answers a channel request when the client wants one.
func reply(req *ssh.Request, ok bool) {
	if req.WantReply {
		_ = req.Reply(ok, nil)
	}
}

const (
	extUserID   = "user-id"
	extUsername = "username"
)

// principalFrom reconstructs the authenticated user from the connection
// permissions stamped during the handshake. SSH always authenticates, so this
// is never the anonymous principal.
func principalFrom(perms *ssh.Permissions) auth.User {
	if perms == nil {
		return auth.Anonymous
	}
	id, err := uuidParse(perms.Extensions[extUserID])
	if err != nil {
		return auth.Anonymous
	}
	return auth.User{ID: id, Username: perms.Extensions[extUsername]}
}
