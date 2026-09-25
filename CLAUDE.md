# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Shared Go library ("bootstrapping service") imported by other services — there is no `main` package or binary.

## Commands

- Verify: `go build ./... && go vet ./... && go test ./...` — tests must pass offline with no credentials (s3/gcs/minio tests are unit-only; don't add tests that need real services).
- Lint: `make lint` (`golangci-lint run ./...`). The existing code has a large lint backlog, so CI only fails on new issues — don't mass-fix unrelated lint in a feature change.
- `make fmt` / `make deps-up` run inside a Docker `golang` image; local `gofmt -s -w` / `golangci-lint fmt ./...` are equivalent and faster.

## Conventions

- Prefer this repo's `errors` package (`github.com/dirty-go/foundation/errors`: `errors.Wrap`, `errors.New`, plus `errors/access`, `errors/data`) over stdlib `errors` in library code.
- Config is loaded from env via `github.com/rez-go/stev` (`stev.LoadEnv(prefix, &cfg)`) with per-package prefixes (`APP_`, `LOG_`, caller-supplied prefixes for REST/CORS and media store).
- Storage backends (`media/store/{s3,gcs,minio,local}`) self-register with `store.RegisterModule` in `init()`; new backends follow the same pattern.

## Git

- Work on a feature branch and open a PR to `main`; never commit directly to `main`. CI (`.github/workflows/tests.yml`) runs `go test -v ./...` and golangci-lint.
