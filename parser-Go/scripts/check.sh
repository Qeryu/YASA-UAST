#!/usr/bin/env bash
# parser-Go 本地检查脚本（替代 push 阶段的 GitHub Action）。
# 内容：Go 静态检查 + 单测 → npm 构建（含 wasm）+ node 测试 → 与冻结原版差分对比。
#
# 用法:
#   bash parser-Go/scripts/check.sh          # 全量
#   bash parser-Go/scripts/check.sh --fast   # 跳过第 3 步（原版差分）
#
# 前置：本工作区建议先 `source /data3/qiusy/yasa-wangkong/env.sh`（Go 1.27.1 / Node 22）；
# 脚本若检测到该 env.sh 会自动 source。
set -euo pipefail

FAST=0
[ "${1:-}" = "--fast" ] && FAST=1

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PG="$REPO_ROOT/parser-Go"
ENV_SH="${YASA_ROOT:-/data3/qiusy/yasa-wangkong}/env.sh"
if [ -f "$ENV_SH" ]; then
  # shellcheck disable=SC1090
  source "$ENV_SH"
fi

cd "$PG"

echo "== [1/3] go vet + go test =="
go vet ./...
go test -count=1 ./...

echo "== [2/3] npm test（pretest 会先 build + build:wasm） =="
if [ ! -d node_modules ]; then
  npm install
fi
npm test

if [ "$FAST" -eq 1 ]; then
  echo "== [3/3] 跳过原版差分（--fast） =="
else
  echo "== [3/3] 与冻结原版差分（单文件逐字节 + 确定性 fixture + wasm） =="
  bash scripts/compare-with-original.sh --with-wasm
fi

echo "ALL LOCAL CHECKS PASSED"
