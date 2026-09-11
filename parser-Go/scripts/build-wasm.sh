#!/usr/bin/env bash
# 为 @ant-yasa/uast-parser-go 构建常驻（长生命周期）wasm parser，并把配套的
# Node runner 放到同一目录。
#
# 可移植：只依赖 PATH 上的 `go`（不需要开发用 env.sh / YASA_ROOT），因此在 CI 与
# file: 方式接入的消费方安装中均可运行。
#
# 产物：parser-Go/dist-wasm/uast4go.wasm, parser-Go/dist-wasm/wasm_exec.js
set -euo pipefail

if ! command -v go >/dev/null 2>&1; then
  echo "错误: PATH 上未找到 'go'（构建 dist-wasm/uast4go.wasm 需要）" >&2
  exit 2
fi

PG="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$PG/dist-wasm"
mkdir -p "$OUT"

# 去掉调试信息并规范化路径，以减小随包分发的 wasm 体积。
( cd "$PG" && GOOS=js GOARCH=wasm go build -buildvcs=false -trimpath -ldflags="-s -w" -o "$OUT/uast4go.wasm" ./wasm )

GOROOT="$(go env GOROOT)"
# Go 1.24+ 的 wasm_exec.js 在 lib/wasm 下；更旧工具链在 misc/wasm 下。
WEXEC="$GOROOT/lib/wasm/wasm_exec.js"
[ -f "$WEXEC" ] || WEXEC="$GOROOT/misc/wasm/wasm_exec.js"
if [ ! -f "$WEXEC" ]; then
  echo "错误: 在 $GOROOT 下未找到 wasm_exec.js（lib/wasm 或 misc/wasm）" >&2
  exit 1
fi
cp "$WEXEC" "$OUT/wasm_exec.js"

echo "已构建  $OUT/uast4go.wasm（$(wc -c < "$OUT/uast4go.wasm") 字节，$(go version)）"
echo "已拷贝  $WEXEC -> $OUT/wasm_exec.js"
