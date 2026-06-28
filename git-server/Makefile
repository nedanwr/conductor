# git-server — single Go module (SPEC §4.11.10).
# Toolchain (buf, sqlc, goose, staticcheck, codegen plugins) is pinned via
# go.mod `tool` directives and invoked through `go tool`, so no global installs.

.DEFAULT_GOAL := build
BIN := bin/git-server
PKG := ./...

# Load .env if present (PG DSN, goose vars) without failing when absent.
-include .env
export

.PHONY: gen build test lint migrate up down checks vet staticcheck buflint

## gen: regenerate protobuf/Connect (buf) and typed queries (sqlc); commit output.
gen:
	go tool buf generate
	@if ls internal/db/queries/*.sql >/dev/null 2>&1; then \
		go tool sqlc generate; \
	else \
		echo "sqlc: no queries yet (skipping until Phase 2)"; \
	fi

## build: compile the single artifact.
build:
	go build -o $(BIN) ./cmd/git-server

## test: run all Go tests.
test:
	go test $(PKG)

## lint: go vet + staticcheck + buf lint + the standing greps (§ Standing checks).
lint: vet staticcheck buflint checks

vet:
	go vet $(PKG)

staticcheck:
	go tool staticcheck $(PKG)

buflint:
	go tool buf lint

## checks: the standing greps that enforce the architectural seams.
checks:
	@echo ">> no-mode-branching (mode-awareness must not leak below internal/app/)"
	@! grep -rn "Mode" internal/ --include='*.go' --exclude-dir=app | grep -v '_test.go' \
		|| { echo "FAIL: mode referenced outside internal/app/"; exit 1; }
	@echo ">> no raw SQL outside migrations/"
	@! grep -rniE "(SELECT|INSERT|UPDATE|DELETE)[[:space:]].*(FROM|INTO|SET)" internal/ --include='*.go' \
		| grep -v '_test.go' | grep -v 'internal/db/queries/' \
		|| { echo "FAIL: raw SQL found in application code"; exit 1; }
	@echo ">> no service package imports a peer's impl"
	@! grep -rn "git-server/internal/\(repostorage\|registry\|cache\)\"" internal/ --include='*.go' \
		| grep -vE "internal/(repostorage|registry|cache|app)/" \
		|| { echo "FAIL: a package imports a service impl directly (use core/ + gen/)"; exit 1; }
	@echo "OK: standing greps clean"

## up: start local Postgres (dev == prod) via docker-compose.
up:
	docker compose up -d

## down: stop local Postgres.
down:
	docker compose down

## migrate: apply goose migrations against the configured Postgres.
migrate:
	go tool goose -dir migrations postgres "$(GOOSE_DBSTRING)" up
