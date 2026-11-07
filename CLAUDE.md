# CLAUDE.md

Backrest is a web UI and orchestrator for [restic](https://restic.net). Go backend, React + TypeScript web UI, API defined in protobuf (Connect RPC). The web UI is embedded into the Go binary.

This repository is a fork of `garethgeorge/backrest`.

## Branch workflow

- `main` tracks upstream. Never commit fork-specific work there.
- `integration` is the fork's development line. It contains all accepted fork changes.
- Feature work: branch from `integration`, open the PR against `integration`.
- Cloud sessions are often created on a branch based on `main`. Before starting, reset the working branch onto `origin/integration` (`git checkout -B <branch> origin/integration`) unless told otherwise.
- Keep changes mergeable with upstream: prefer additive proto changes (new fields and RPCs, never renumber or repurpose existing ones), new views over rewrites of existing ones, and small focused commits.
- Planned work lives in GitHub issues on `edobez/backrest`. Reference the issue in commits and PRs (`Closes #N`).

## Layout

- `cmd/backrest/`: main binary. `version` and `commit` are injected with `-ldflags` in release builds.
- `internal/orchestrator/`: task scheduling and execution. `tasks/` holds one file per task type (backup, forget, prune, check, index snapshots, restore, stats). `repo/` wraps restic for a single repository.
- `internal/api/`: Connect RPC handlers (`backresthandler.go`) and multi-instance sync (`syncapi/`).
- `internal/config/`: config loading, validation and migrations.
- `internal/oplog/`: operation log storage (history of all operations and indexed snapshots).
- `pkg/restic/`: thin wrapper around the restic CLI.
- `proto/`: protobuf definitions. `v1/config.proto` is the config schema, `v1/service.proto` the API.
- `gen/go/`, `webui/gen/ts/`: generated code, committed to the repo. Do not edit by hand.
- `webui/src/`: UI. `features/` per area (plans, repositories, operations, dashboard, settings), `lib/` shared helpers, `state/` client state.
- `webui/messages/*.json`: i18n strings (paraglide/inlang). `en.json` is the source of truth.
- `docs/`: documentation site (VitePress).

## Domain notes

- Snapshots created by Backrest carry a `plan:<id>` tag and a `created-by:<instance>` tag. Snapshots without a plan tag are indexed under the pseudo plan `_unassociated_` (`internal/orchestrator/tasks/task.go`).
- restic has no repository ownership: any key can forget and prune. Any feature that deletes snapshots must be scoped explicitly (by plan, tag, host or path) and must not touch snapshots it does not manage.

## Build and test

Go backend (matches CI in `.github/workflows/test.yml`):

```sh
# The Go build embeds webui/dist. Without a built UI, create a placeholder:
mkdir -p webui/dist && gzip -c /dev/null > webui/dist/index.html.gz

go build ./...
go test ./...                       # CI runs: gotestsum ./... -- --race
go test ./internal/orchestrator/... # faster, targeted
```

Some Go tests download a pinned restic binary on first run (`internal/resticinstaller`), so they need network access to GitHub releases.

Web UI (pnpm, run from `webui/`):

```sh
pnpm install --frozen-lockfile
pnpm run check      # tsc typecheck
pnpm test           # compiles i18n, then vitest
pnpm run build      # production build into webui/dist
pnpm start          # dev server, expects a backend on localhost:9898
pnpm run e2e        # Playwright, see webui/e2e/README.md
```

Gotcha: `pnpm run compile-i18n` (run by `pnpm test`) downloads inlang plugins from `cdn.jsdelivr.net`. If that host is unreachable, it still reports success but generates no messages, and tests fail with errors like `TypeError: login_title is not a function`. That is a network problem, not a code problem.

Protobuf: after editing `proto/`, regenerate with `cd proto && ./update.sh` (runs `buf generate`). Requires `buf`, `protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-connect-go` and `protoc-gen-es`; the nix dev shell (`nix develop`) provides them. Commit the regenerated `gen/go/` and `webui/gen/ts/` together with the proto change.

## Conventions

- Commit messages follow Conventional Commits (`feat:`, `fix:`, `chore:`), as in the upstream history.
- User-facing UI strings go through paraglide messages, not hardcoded text. Add new keys to `webui/messages/en.json`.
- Forms with Save/Discard (`TwoPaneModal`, e.g. `SettingsModal.tsx`) apply nothing until Save. This includes browser-local preferences stored in localStorage: keep edits as a draft in the modal, count them in `dirty`/`dirtyCount`, apply them on Save, reset them on Discard. Exception: the language selector applies immediately (upstream behavior).
- Format the web UI with `pnpm run format` (prettier) and Go with `gofmt`.
- Before pushing, run at least `go build ./...`, the Go tests for the packages you touched, and `pnpm run check` for UI changes.
