---
name: verify
description: Build, vet, test, and lint the foundation library. Use after making Go changes, before committing or opening a PR.
---

Run from the repo root, in order, stopping at the first failure and reporting the exact errors:

1. `go build ./...`
2. `go vet ./...`
3. `go test ./...` (must pass offline — no credentials or external services)
4. `golangci-lint run ./...` if `golangci-lint` is installed

Report a one-line pass/fail per step. Do not fix failures unless asked.
