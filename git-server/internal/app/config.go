package app

import (
	"os"
	"strconv"
	"time"
)

// Config is the full runtime configuration surface, resolved from the
// environment. Secrets (the Postgres DSN, the SSH host key, TLS material) are
// referenced by value or path here but never embedded in the repository; they
// arrive from the environment or mounted files at launch.
//
// One Config describes a process in any role. Which fields matter depends on the
// selected mode — an edge process reads the listen addresses and host key, a
// storage process reads the storage root — but the surface is uniform so the same
// binary launches into any role from one configuration shape.
type Config struct {
	// Mode is the runtime role this process plays.
	Mode Mode

	// NodeID identifies this process as a storage node in the placement
	// directory. A single co-located process is the one node; a split fleet gives
	// each storage node a stable id.
	NodeID string

	// StorageRoot is the directory under which bare repositories live. Read by the
	// roles that own disk (repo-storage, all).
	StorageRoot string

	// DatabaseDSN is the Postgres connection string for the control-plane tables.
	// Read by the roles that touch them (gateway, registry, all) and by admin.
	DatabaseDSN string

	// SSHAddr and HTTPSAddr are the git edge listen addresses. Read by the roles
	// that terminate client connections (gateway, all).
	SSHAddr   string
	HTTPSAddr string

	// ConnectAddr is the internal Connect listen address for a single-service
	// process in a split deployment; co-located peers never use it.
	ConnectAddr string

	// RepoStorageURL, RegistryURL, and CacheURL are the Connect endpoints of
	// remote peers, used only when a role's collaborators run in other processes.
	// Empty means the peer is co-located and wired in-process.
	RepoStorageURL string
	RegistryURL    string
	CacheURL       string

	// SSHHostKeyPath is the PEM-encoded host key presented to SSH clients. When
	// empty an ephemeral key is generated, acceptable only for development.
	SSHHostKeyPath string

	// TLSCertPath and TLSKeyPath are the HTTPS edge certificate and key. When both
	// are empty the edge serves plain HTTP, acceptable only for development.
	TLSCertPath string
	TLSKeyPath  string

	// PlacementCacheTTL bounds how long a resolved placement is trusted before the
	// registry is consulted again.
	PlacementCacheTTL time.Duration

	// LogLevel is the minimum log severity emitted.
	LogLevel string
}

// LoadConfig resolves configuration for the given mode from the environment,
// applying development-friendly defaults for anything unset.
func LoadConfig(mode Mode) Config {
	return Config{
		Mode:              mode,
		NodeID:            envOr("GITSERVER_NODE_ID", "node-local"),
		StorageRoot:       envOr("GITSERVER_STORAGE_ROOT", "./data/repos"),
		DatabaseDSN:       os.Getenv("DATABASE_URL"),
		SSHAddr:           envOr("GITSERVER_SSH_ADDR", ":2222"),
		HTTPSAddr:         envOr("GITSERVER_HTTPS_ADDR", ":8080"),
		ConnectAddr:       envOr("GITSERVER_CONNECT_ADDR", ":8443"),
		RepoStorageURL:    os.Getenv("GITSERVER_REPO_STORAGE_URL"),
		RegistryURL:       os.Getenv("GITSERVER_REGISTRY_URL"),
		CacheURL:          os.Getenv("GITSERVER_CACHE_URL"),
		SSHHostKeyPath:    os.Getenv("GITSERVER_SSH_HOST_KEY"),
		TLSCertPath:       os.Getenv("GITSERVER_TLS_CERT"),
		TLSKeyPath:        os.Getenv("GITSERVER_TLS_KEY"),
		PlacementCacheTTL: envDuration("GITSERVER_PLACEMENT_CACHE_TTL", 30*time.Second),
		LogLevel:          envOr("GITSERVER_LOG_LEVEL", "info"),
	}
}

// envOr returns the environment value for key, or def when it is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envDuration parses a duration from the environment, falling back to def when
// the value is unset or unparseable.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	return def
}
