#!/usr/bin/env bash
# Build the resident (long-lived) wasm parser for @ant-yasa/uast-parser-go and
# stage the matching Node runner next to it.
#
# Portable: relies only on a `go` on PATH (no dev env.sh / YASA_ROOT needed), so
# it works in CI and in a file:-linked consumer install.
#
# Output: parser-Go/dist-wasm/uast4go.wasm, parser-Go/dist-wasm/wasm_exec.js
set -euo pipefail

if ! command -v go >/dev/null 2>&1; then
  echo "error: 'go' not found on PATH (required to build dist-wasm/uast4go.wasm)" >&2
  exit 2
fi

PG="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$PG/dist-wasm"
mkdir -p "$OUT"

# Strip debug info and normalize paths to keep the shipped wasm small.
( cd "$PG" && GOOS=js GOARCH=wasm go build -buildvcs=false -trimpath -ldflags="-s -w" -o "$OUT/uast4go.wasm" ./wasm )

GOROOT="$(go env GOROOT)"
# Go 1.24+ ships wasm_exec.js under lib/wasm; older toolchains under misc/wasm.
WEXEC="$GOROOT/lib/wasm/wasm_exec.js"
[ -f "$WEXEC" ] || WEXEC="$GOROOT/misc/wasm/wasm_exec.js"
if [ ! -f "$WEXEC" ]; then
  echo "error: wasm_exec.js not found under $GOROOT (lib/wasm or misc/wasm)" >&2
  exit 1
fi
cp "$WEXEC" "$OUT/wasm_exec.js"

echo "built  $OUT/uast4go.wasm ($(wc -c < "$OUT/uast4go.wasm") bytes, $(go version))"
echo "copied $WEXEC -> $OUT/wasm_exec.js"
