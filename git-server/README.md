# git-server

The from-scratch git server for Conductor's git platform. A **single Go
artifact** that launches into a runtime role via `--mode`
(`gateway | repo-storage | cache | registry | all`); `--mode=all` is the
single-binary dev case with every service wired in-process.

This module lives at the repo root, **outside** Bun's workspace globs
(`packages/*`, `apps/*`) — the monorepo is polyglot and the Go toolchain owns
this tree.

## Toolchain

Pinned via `go.mod` `tool` directives, invoked through `go tool` — no global
installs required:

- **Connect-Go over protobuf** (`buf`) — service transport
- **sqlc** — typed queries
- **goose** — migrations (raw SQL lives only in `migrations/`)
- **Postgres** — dev == prod

## Commands

```sh
make up        # start local Postgres (docker-compose)
make migrate   # apply goose migrations
make gen       # buf generate + sqlc generate (commit the output)
make build     # compile the single artifact -> bin/git-server
make test      # go test ./...
make lint      # go vet + staticcheck + buf lint + standing greps
```

Copy `.env.example` to `.env` for local config.

## Layout

The load-bearing rules:

- `internal/app/` is the **only mode-aware package** (the wiring root).
- `internal/core/` holds service **interfaces** + serializable boundary types;
  impls live in their own packages and a client adapter never imports a peer's
  impl.
- `proto/` and `migrations/` are the **only** places contracts and raw SQL are
  authored.

## Status

Phase 0 (scaffold & toolchain) of `GIT_SERVER_BUILD_PLAN.md`: a compiling empty
skeleton. No services are wired yet.
