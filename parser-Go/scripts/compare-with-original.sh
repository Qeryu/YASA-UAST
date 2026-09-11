#!/usr/bin/env bash
# 对比"冻结的原版 uastgo"与当前工作树的输出（重构期间保留原版，便于逐字节比对）。
#
# 用法:
#   bash parser-Go/scripts/compare-with-original.sh [--ref <git-ref>] [--with-wasm]
#
# 默认 ref: baseline/uastgo-07e3823（不存在时回退到 07e3823）
# 做四件事:
#   1. git archive 导出 ref 的 parser-Go 到临时目录并构建原版 native
#   2. 构建当前工作树 native
#   3. 单文件模式逐字节对比 examples/ 下所有 .go（确定性）
#   4. 项目模式（-rootDir=examples）忽略 tmpN 后对比；--with-wasm 时再补一轮 wasm 单文件对比
# 有差异时退出码非 0。
set -euo pipefail

REF="baseline/uastgo-07e3823"
WITH_WASM=0
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) REF="${2:-}"; shift 2 ;;
    --with-wasm) WITH_WASM=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

source "${YASA_ROOT:-/data3/qiusy/yasa-wangkong}/env.sh"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
PG="$REPO/parser-Go"
OUT="$(mktemp -d)"
BASE="$(mktemp -d)"
trap 'rm -rf "$OUT" "$BASE"' EXIT

if ! git -C "$REPO" rev-parse --verify --quiet "$REF" >/dev/null; then
  echo "[warn] ref '$REF' 不存在，回退到 07e3823"
  REF="07e3823"
fi

echo "[1/4] 导出原版 ($REF) 并构建 native"
git -C "$REPO" archive "$REF" parser-Go | tar -x -C "$BASE"
( cd "$BASE/parser-Go" && CGO_ENABLED=0 go build -buildvcs=false -o "$OUT/uast4go-original" . )

echo "[2/4] 构建当前工作树 native"
( cd "$PG" && CGO_ENABLED=0 go build -buildvcs=false -o "$OUT/uast4go-current" . )

cd "$PG"
files="$(find examples -name '*.go' | sort)"
single_total=0
single_diff=0
diff_list=""
while IFS= read -r f; do
  [ -n "$f" ] || continue
  single_total=$((single_total + 1))
  "$OUT/uast4go-original" -single -rootDir="$f" -output="$OUT/o.json" >/dev/null 2>&1 || true
  "$OUT/uast4go-current" -single -rootDir="$f" -output="$OUT/c.json" >/dev/null 2>&1 || true
  if ! cmp -s "$OUT/o.json" "$OUT/c.json"; then
    single_diff=$((single_diff + 1))
    diff_list="$diff_list $f"
  fi
done <<< "$files"
echo "[3/4] 单文件对比: $((single_total - single_diff))/$single_total 一致"
if [ -n "$diff_list" ]; then
  echo "  差异:"
  for f in $diff_list; do echo "    -$f"; done
fi

echo "[4/4] 项目模式（忽略 tmpN；信息性，不计入退出码）"
proj_rc=0
"$OUT/uast4go-original" -rootDir=examples -output="$OUT/o-proj.json" >/dev/null 2>&1 || true
"$OUT/uast4go-current" -rootDir=examples -output="$OUT/c-proj.json" >/dev/null 2>&1 || true
sed 's/tmp[0-9]*/tmpX/g' "$OUT/o-proj.json" > "$OUT/o.norm"
sed 's/tmp[0-9]*/tmpX/g' "$OUT/c-proj.json" > "$OUT/c.norm"
if cmp -s "$OUT/o.norm" "$OUT/c.norm"; then
  echo "  一致"
else
  echo "  有差异 —— 这是原版既有缺陷：目录内随机选包（main.go:27-29，本次不修，见 wasm-plan §5.4）；"
  echo "  项目模式的严格对比不可用，以单文件逐字节 + Engine benchmark 为准。"
  proj_rc=1
fi

if [ "$WITH_WASM" -eq 1 ]; then
  echo "[+wasm] 构建当前 wasm 并对比单文件"
  ( cd "$PG" && GOOS=js GOARCH=wasm go build -buildvcs=false -o "$OUT/uast4go-current.wasm" . )
  WEXEC="$GOROOT/misc/wasm/wasm_exec_node.js"
  [ -f "$WEXEC" ] || WEXEC="$GOROOT/lib/wasm/wasm_exec_node.js"
  w_total=0
  w_diff=0
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    w_total=$((w_total + 1))
    "$OUT/uast4go-original" -single -rootDir="$f" -output="$OUT/o.json" >/dev/null 2>&1 || true
    node "$WEXEC" "$OUT/uast4go-current.wasm" -single -rootDir="$f" -output="$OUT/c.json" >/dev/null 2>&1 || true
    cmp -s "$OUT/o.json" "$OUT/c.json" || w_diff=$((w_diff + 1))
  done <<< "$files"
  echo "  wasm 单文件: $((w_total - w_diff))/$w_total 一致"
  [ "$w_diff" -eq 0 ] || single_diff=$((single_diff + 1))
fi

echo
if [ "$single_diff" -eq 0 ]; then
  echo "RESULT: PASS (单文件严格对比通过；项目模式为信息性)"
  exit 0
fi
echo "RESULT: DIFF (单文件严格对比存在差异；project_rc=$proj_rc 仅供参考)"
exit 1
