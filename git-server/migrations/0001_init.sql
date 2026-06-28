-- +goose Up
-- +goose StatementBegin

-- users — the identity each SSH key and access token resolves to.
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username   TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ssh_keys — public keys for SSH public-key authentication. The fingerprint is
-- the lookup key on the authN path; the raw key blob is retained for matching.
CREATE TABLE ssh_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ssh_keys_user_id_idx ON ssh_keys (user_id);

-- access_tokens — HTTPS bearer tokens. Only the hash is stored, never the token.
CREATE TABLE access_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX access_tokens_user_id_idx ON access_tokens (user_id);

-- repos — the internal identity behind the edge; owner/name resolves to the UUID.
CREATE TABLE repos (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner          TEXT NOT NULL,
    name           TEXT NOT NULL,
    visibility     TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public')),
    default_branch TEXT NOT NULL DEFAULT 'main',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner, name)
);

-- repo_permissions — per-user access level on a repo.
CREATE TABLE repo_permissions (
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    repo_id UUID NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    level   TEXT NOT NULL CHECK (level IN ('read', 'write', 'admin')),
    PRIMARY KEY (user_id, repo_id)
);

CREATE INDEX repo_permissions_repo_id_idx ON repo_permissions (repo_id);

-- repo_placement — repo UUID → storage node directory.
CREATE TABLE repo_placement (
    repo_id         UUID PRIMARY KEY REFERENCES repos (id) ON DELETE CASCADE,
    storage_node_id TEXT NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS repo_placement;
DROP TABLE IF EXISTS repo_permissions;
DROP TABLE IF EXISTS repos;
DROP TABLE IF EXISTS access_tokens;
DROP TABLE IF EXISTS ssh_keys;
DROP TABLE IF EXISTS users;

-- +goose StatementEnd
