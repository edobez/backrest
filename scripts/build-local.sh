#!/usr/bin/env bash
#
# Full local build of backrest: web UI + Go binary with the UI embedded.
#
# Usage: scripts/build-local.sh [options]
#
#   --skip-ui        Reuse the existing webui/dist instead of rebuilding the UI
#   --test           Run Go tests and the UI typecheck/tests after building
#   --run [DIR]      Start the built binary afterwards. Config and data go in
#                    DIR (default: .local-run/ in the repo root, kept across
#                    runs). Pass --run --fresh for a throwaway temp dir.
#   --fresh          With --run, use a temporary dir that is deleted on exit
#   -o, --output P   Output binary path (default: ./backrest)
#   -h, --help       Show this help
#
# Extra arguments after `--` are passed to backrest when using --run,
# e.g. scripts/build-local.sh --run -- --bind-address=:9898

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SKIP_UI=0
RUN_TESTS=0
RUN=0
FRESH=0
RUN_DIR="$ROOT/.local-run"
OUTPUT="$ROOT/backrest"
BACKREST_ARGS=()

usage() { sed -n '3,18p' "$0" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-ui) SKIP_UI=1 ;;
    --test) RUN_TESTS=1 ;;
    --run)
      RUN=1
      if [[ $# -gt 1 && "$2" != -* ]]; then RUN_DIR="$2"; shift; fi
      ;;
    --fresh) FRESH=1 ;;
    -o|--output) OUTPUT="$2"; shift ;;
    -h|--help) usage; exit 0 ;;
    --) shift; BACKREST_ARGS=("$@"); break ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
  shift
done

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

# pnpm: use the installed one, otherwise fall back to npx (pinned to 9, as in CI).
if command -v pnpm >/dev/null 2>&1; then
  PNPM=(pnpm)
elif command -v npx >/dev/null 2>&1; then
  PNPM=(npx --yes pnpm@9)
else
  echo "pnpm (or node/npx) is required to build the web UI" >&2
  exit 1
fi
command -v go >/dev/null 2>&1 || { echo "go is required" >&2; exit 1; }

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

if [[ $SKIP_UI -eq 0 ]]; then
  step "Installing web UI dependencies"
  (cd webui && "${PNPM[@]}" install --frozen-lockfile)

  step "Building web UI (webui/dist)"
  (cd webui && "${PNPM[@]}" run clean && "${PNPM[@]}" run build)

  # vite compiles the paraglide messages; if the inlang CDN was unreachable
  # the build "succeeds" with no messages. Catch that here.
  if [[ ! -s webui/src/paraglide/messages.js ]]; then
    echo "warning: webui/src/paraglide/messages.js is missing or empty;" \
         "i18n compilation may have failed (is cdn.jsdelivr.net reachable?)" >&2
  fi
elif [[ ! -d webui/dist ]] || [[ -z "$(ls -A webui/dist 2>/dev/null)" ]]; then
  echo "--skip-ui given but webui/dist is empty; run without --skip-ui first" >&2
  exit 1
fi

step "Building Go binary ($VERSION, $COMMIT) -> $OUTPUT"
CGO_ENABLED=0 go build \
  -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" \
  -o "$OUTPUT" ./cmd/backrest

if [[ $RUN_TESTS -eq 1 ]]; then
  step "Running Go tests"
  go test ./...

  step "Typechecking and testing web UI"
  (cd webui && "${PNPM[@]}" run check && "${PNPM[@]}" test)
fi

step "Done: $OUTPUT"
"$OUTPUT" --version || true

if [[ $RUN -eq 1 ]]; then
  if [[ $FRESH -eq 1 ]]; then
    RUN_DIR="$(mktemp -d)"
    trap 'echo "Removing $RUN_DIR"; rm -rf "$RUN_DIR"' EXIT
  fi
  mkdir -p "$RUN_DIR"
  step "Starting backrest (config/data in $RUN_DIR) on http://localhost:9898"
  "$OUTPUT" \
    --config-file="$RUN_DIR/config.json" \
    --data-dir="$RUN_DIR/data" \
    ${BACKREST_ARGS[@]+"${BACKREST_ARGS[@]}"}
fi
