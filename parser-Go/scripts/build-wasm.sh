#!/usr/bin/env bash
# Build the resident (long-lived) wasm parser for @ant-yasa/uast-parser-go and
# stage the matching Node runner next to it.
#
# Output: parser-Go/dist-wasm/uast4go.wasm, parser-Go/dist-wasm/wasm_exec.js
set -euo pipefail

source "${YASA_ROOT:-/data3/qiusy/yasa-wangkong}/env.sh"

PG="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$PG/dist-wasm"
mkdir -p "$OUT"

( cd "$PG" && GOOS=js GOARCH=wasm go build -buildvcs=false -o "$OUT/uast4go.wasm" ./wasm )

# Go 1.24+ ships wasm_exec.js under lib/wasm; older toolchains under misc/wasm.
WEXEC="$GOROOT/lib/wasm/wasm_exec.js"
[ -f "$WEXEC" ] || WEXEC="$GOROOT/misc/wasm/wasm_exec.js"
if [ ! -f "$WEXEC" ]; then
  echo "wasm_exec.js not found under $GOROOT (lib/wasm or misc/wasm)" >&2
  exit 1
fi
cp "$WEXEC" "$OUT/wasm_exec.js"

echo "built  $OUT/uast4go.wasm ($(wc -c < "$OUT/uast4go.wasm") bytes)"
echo "copied $WEXEC -> $OUT/wasm_exec.js"
