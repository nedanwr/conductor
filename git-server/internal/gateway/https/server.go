package https

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
)

// TokenAuthenticator resolves an HTTPS access token to a user. It is the HTTP
// half of authN; the same user identity and authZ source of truth back every
// transport.
type TokenAuthenticator interface {
	FromToken(ctx context.Context, raw string) (auth.User, error)
}

// Handler is the smart-HTTP terminator. It is a plain http.Handler so it can be
// mounted on any server (TLS in production, plain HTTP in dev); the surrounding
// listener owns TLS material.
type Handler struct {
	gw    *gateway.Gateway
	authn TokenAuthenticator
}

// NewHandler builds the smart-HTTP handler over the Gateway core and the token
// authenticator.
func NewHandler(gw *gateway.Gateway, authn TokenAuthenticator) *Handler {
	return &Handler{gw: gw, authn: authn}
}

// ServeHTTP dispatches the three smart-HTTP endpoints: the GET ref
// advertisement and the two RPC posts. Anything else is a 404.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner, repo, endpoint, ok := parsePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && endpoint == "info/refs":
		h.advertise(w, r, owner, repo)
	case r.Method == http.MethodPost && (endpoint == "git-upload-pack" || endpoint == "git-receive-pack"):
		h.rpc(w, r, owner, repo, endpoint)
	default:
		http.NotFound(w, r)
	}
}

// advertise serves GET info/refs: the client asks which refs and capabilities a
// service offers before negotiating. authZ runs here too — advertising
// receive-pack requires write, so an unauthorized client cannot even see the
// push advertisement.
func (h *Handler) advertise(w http.ResponseWriter, r *http.Request, owner, repo string) {
	service := r.URL.Query().Get("service")
	op, err := gateway.OperationForService(service)
	if err != nil {
		writeError(w, err, false)
		return
	}

	in := h.intake(r, owner, repo, op)
	in.Protocol.Stateless = true
	in.Protocol.AdvertiseRefs = true

	dw := &deferredWriter{w: w, onFirst: func(rw http.ResponseWriter) error {
		setNoCache(rw)
		rw.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
		rw.WriteHeader(http.StatusOK)
		_, werr := rw.Write(serviceHeader(service))
		return werr
	}}

	if serr := h.gw.Serve(r.Context(), in, r.Body, dw); serr != nil {
		writeError(w, serr, dw.started)
	}
}

// rpc serves the POST upload-pack/receive-pack round: the request body carries
// the client's negotiation, the response body carries the pack/result.
func (h *Handler) rpc(w http.ResponseWriter, r *http.Request, owner, repo, service string) {
	op, err := gateway.OperationForService(service)
	if err != nil {
		writeError(w, err, false)
		return
	}

	in := h.intake(r, owner, repo, op)
	in.Protocol.Stateless = true

	dw := &deferredWriter{w: w, onFirst: func(rw http.ResponseWriter) error {
		setNoCache(rw)
		rw.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-result", service))
		rw.WriteHeader(http.StatusOK)
		return nil
	}}

	if serr := h.gw.Serve(r.Context(), in, r.Body, dw); serr != nil {
		writeError(w, serr, dw.started)
	}
}

// intake assembles the transport-neutral intake from an HTTP request: the
// authenticated principal, the requested operation, and the negotiated protocol
// version. The transport-specific concerns end here.
func (h *Handler) intake(r *http.Request, owner, repo string, op gitreq.Operation) gateway.Intake {
	return gateway.Intake{
		Owner:         owner,
		Repo:          repo,
		Operation:     op,
		Principal:     h.principal(r),
		Protocol:      gitreq.ProtocolParams{Version: protocolVersion(r)},
		CorrelationID: r.Header.Get("Git-Protocol-Correlation-Id"),
	}
}

// principal authenticates the request from its Authorization header, falling
// back to the anonymous principal when none is present (a public fetch). A
// malformed or unknown credential resolves to anonymous as well; authZ then
// decides whether anonymous access is allowed, yielding a 401 that prompts the
// client to retry with credentials.
func (h *Handler) principal(r *http.Request) auth.User {
	raw, ok := bearerOrBasic(r.Header.Get("Authorization"))
	if !ok {
		return auth.Anonymous
	}
	user, err := h.authn.FromToken(r.Context(), raw)
	if err != nil {
		return auth.Anonymous
	}
	return user
}

// bearerOrBasic extracts the token from either an HTTP Bearer header or the
// password field of HTTP Basic auth (git sends the token as the password). It
// returns false when no usable credential is present.
func bearerOrBasic(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	scheme, rest, found := strings.Cut(header, " ")
	if !found {
		return "", false
	}
	switch strings.ToLower(scheme) {
	case "bearer":
		return strings.TrimSpace(rest), rest != ""
	case "basic":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
		if err != nil {
			return "", false
		}
		_, pass, _ := strings.Cut(string(decoded), ":")
		return pass, pass != ""
	default:
		return "", false
	}
}

// protocolVersion reads the negotiated git protocol version from the Git-Protocol
// header, defaulting to 0 (the fallback) when v2 is not requested.
func protocolVersion(r *http.Request) int {
	for _, field := range strings.Split(r.Header.Get("Git-Protocol"), ":") {
		if strings.TrimSpace(field) == "version=2" {
			return 2
		}
	}
	return 0
}

// parsePath splits a smart-HTTP path "/owner/repo.git/<endpoint>" into its parts.
// It returns ok=false for any path not addressing a ".git" repo endpoint.
func parsePath(p string) (owner, repo, endpoint string, ok bool) {
	const marker = ".git/"
	idx := strings.Index(p, marker)
	if idx < 0 {
		return "", "", "", false
	}
	left := strings.TrimPrefix(p[:idx], "/")
	endpoint = p[idx+len(marker):]
	owner, repo, found := strings.Cut(left, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", "", false
	}
	return owner, repo, endpoint, true
}

// setNoCache marks a response uncacheable; git advertisements and results must
// never be served from a cache.
func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
}

// writeError maps a typed error to an HTTP status. Once the response body has
// started (streaming has begun) the status is already committed, so there is
// nothing to write but a torn connection; we only set a status when the headers
// are still open.
func writeError(w http.ResponseWriter, err error, started bool) {
	if started {
		return
	}
	status := httpStatusFor(giterr.KindOf(err))
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
	}
	http.Error(w, err.Error(), status)
}

// httpStatusFor maps a typed error kind to an HTTP status. Unauthorized becomes
// 401 so the client is prompted for credentials and retries; the rest follow the
// usual git smart-HTTP conventions.
func httpStatusFor(kind giterr.Kind) int {
	switch kind {
	case giterr.KindUnauthorized:
		return http.StatusUnauthorized
	case giterr.KindRepoNotFound:
		return http.StatusNotFound
	case giterr.KindRefRejected:
		return http.StatusForbidden
	case giterr.KindPlacementMiss:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
