# Contributing

## Requirements

- All code must have tests written with [Ginkgo](https://onsi.github.io/ginkgo/) v2 and Gomega.
- Tests must pass with the race detector enabled (`-race`).
- `go vet ./...` must be clean.
- `golangci-lint run ./...` must be clean.
- Generated mocks (`internal/testutil/`) are produced with `go.uber.org/mock/mockgen` in source mode. Regenerate with `make proto-gen` before submitting changes to interfaces.
- Generated protobuf code (`gen/`) is committed to the repository. Regenerate with `make proto-gen` when `proto/token_engine.proto` changes.
- Update `CHANGELOG.md` under `[Unreleased]` for any user-visible change.

## Workflow

```bash
git clone https://github.com/aetomala/token-engine.git
cd token-engine

# Run the full CI pipeline locally before pushing
./run-ci-locally.sh
```

## Branching

- Branch off `dev`: `git checkout -b <area>/short-description`
- PRs target `dev`, never `main`.
- One logical concern per PR.
- Branch name format: `feat/`, `fix/`, `refactor/`, `infra/`, `docs/`

## Code Conventions

Follow the patterns established in the existing packages:

- Observability (logger, metrics, tracer) injected via config struct — never constructed inside components.
- NoOp implementations exist for every interface — constructors inject NoOp when a config field is nil.
- No nil-checks for observability fields at call sites.
- Error sentinels declared at package level in `var` blocks with a group comment.
- `go.uber.org/mock` for all mocks; source-mode generation only.

## Architecture

Read [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md) before making structural changes.
Check [doc/adr/](doc/adr/) for decisions already made — new structural changes should be accompanied by an ADR.
