package service

import (
	"fmt"
	"net/url"
	"strings"
)

// trustDomain is the cluster's identity namespace. Every service identity is a
// URI under this domain, encoded as a SPIFFE-style SAN in the leaf certificate
// so a peer's role and node are carried by the credential itself rather than
// asserted out of band.
const trustDomain = "gitserver"

// Name is a service principal: the role a process plays and the node it runs as.
// It is the stable, human-meaningful identity bound into an issued certificate
// and recovered from a verified peer. Role lets a callee reason about which kind
// of service is calling (a storage node expects gateways, not other storage
// nodes); NodeID distinguishes instances of that role.
type Name struct {
	Role   string
	NodeID string
}

// URI renders the name as its canonical SPIFFE-style identifier,
// spiffe://<trust-domain>/<role>/<node-id>, the form embedded in a certificate's
// URI SAN.
func (n Name) URI() *url.URL {
	return &url.URL{
		Scheme: "spiffe",
		Host:   trustDomain,
		Path:   "/" + n.Role + "/" + n.NodeID,
	}
}

// String returns the canonical URI text.
func (n Name) String() string { return n.URI().String() }

// valid reports whether both components are present, since an identity missing
// either half cannot be matched or attributed.
func (n Name) valid() bool { return n.Role != "" && n.NodeID != "" }

// ParseName recovers a Name from a SPIFFE-style URI, rejecting anything not in
// this trust domain or not shaped <role>/<node-id>. It is the inverse of URI and
// the single place a certificate's SAN is interpreted.
func ParseName(u *url.URL) (Name, error) {
	if u == nil {
		return Name{}, fmt.Errorf("service identity: nil URI")
	}
	if u.Scheme != "spiffe" || u.Host != trustDomain {
		return Name{}, fmt.Errorf("service identity: not a %s identity: %q", trustDomain, u)
	}
	role, node, ok := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	n := Name{Role: role, NodeID: node}
	if !ok || !n.valid() {
		return Name{}, fmt.Errorf("service identity: malformed identity path: %q", u.Path)
	}
	return n, nil
}
