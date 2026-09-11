#!/usr/bin/env bash
# 对比"冻结的原版 uastgo"与当前工作树的输出（重构期间保留原版，便于逐字节比对）。
#
# 用法:
#   bash parser-Go/scripts/compare-with-original.sh [--ref <git-ref>] [--with-wasm]
#
# 默认 ref: baseline/uastgo-07e3823（不存在时回退到 07e3823）
# 做五件事:
#   1. git archive 导出 ref 的 parser-Go 到临时目录并构建原版 native
#   2. 构建当前工作树 native
#   3. 单文件模式逐字节对比 examples/ 下所有 .go（确定性；计入退出码）
#   4. 确定性项目 fixture（单包 module）忽略 tmpN 后逐字节对比（计入退出码）
#   5. 项目模式 -rootDir=examples 仅信息性对比（原版选包随机 + 跨文件 map 顺序）
#      --with-wasm 时再补 wasm 单文件对比
#
# 每次调用前都会 rm -f 输出文件；cmp 前断言文件存在且非空，并记录双方退出码。
# 差异分类:
#   - "原始失败（无输出）"：原版退出码非 0 或没有产出输出；
#   - "输出差异"：双方都有产出但内容不同。
# 退出码: 单文件严格对比 + 确定性项目 fixture 有差异时非 0。
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
FIXTURE="$(mktemp -d)"
trap 'rm -rf "$OUT" "$BASE" "$FIXTURE"' EXIT

if ! git -C "$REPO" rev-parse --verify --quiet "$REF" >/dev/null; then
  echo "[warn] ref '$REF' 不存在，回退到 07e3823"
  REF="07e3823"
fi

echo "[1/5] 导出原版 ($REF) 并构建 native"
git -C "$REPO" archive "$REF" parser-Go | tar -x -C "$BASE"
( cd "$BASE/parser-Go" && CGO_ENABLED=0 go build -buildvcs=false -o "$OUT/uast4go-original" . )

echo "[2/5] 构建当前工作树 native"
( cd "$PG" && CGO_ENABLED=0 go build -buildvcs=false -o "$OUT/uast4go-current" . )

cd "$PG"
files="$(find examples -name '*.go' | sort)"

# ---- 3) 单文件逐字节对比（严格） -----------------------------------------------
single_total=0
single_match=0
single_diff=0
single_presence=0
single_both_no_output=0
presence_list=""
both_no_output_list=""

while IFS= read -r f; do
  [ -n "$f" ] || continue
  single_total=$((single_total + 1))
  rm -f "$OUT/o.json" "$OUT/c.json" "$OUT/o.err" "$OUT/c.err"
  o_rc=0
  "$OUT/uast4go-original" -single -rootDir="$f" -output="$OUT/o.json" >"$OUT/o.log" 2>"$OUT/o.err" || o_rc=$?
  c_rc=0
  "$OUT/uast4go-current" -single -rootDir="$f" -output="$OUT/c.json" >"$OUT/c.log" 2>"$OUT/c.err" || c_rc=$?
  o_ok=1; [ -s "$OUT/o.json" ] || o_ok=0
  c_ok=1; [ -s "$OUT/c.json" ] || c_ok=0
  if [ "$o_ok" -eq 0 ] && [ "$c_ok" -eq 0 ]; then
    single_both_no_output=$((single_both_no_output + 1))
    both_no_output_list="$both_no_output_list $f"
    echo "  [both-no-output] $f (orig_rc=$o_rc current_rc=$c_rc)"
  elif [ "$o_ok" -eq 0 ] || [ "$c_ok" -eq 0 ]; then
    single_presence=$((single_presence + 1))
    presence_list="$presence_list $f"
    echo "  [no-output] $f (orig_rc=$o_rc orig_out=$o_ok current_rc=$c_rc current_out=$c_ok)"
  elif cmp -s "$OUT/o.json" "$OUT/c.json"; then
    single_match=$((single_match + 1))
  else
    single_diff=$((single_diff + 1))
    echo "  [diff] $f (orig_rc=$o_rc current_rc=$c_rc)"
  fi
done <<< "$files"

echo "[3/5] 单文件逐字节对比: $single_match/$single_total 一致" \
     "(输出差异=$single_diff, 一方无输出=$single_presence, 双方均无输出=$single_both_no_output)"
if [ -n "$presence_list" ]; then
  echo "  原始失败（无输出）/ 一方无输出:"
  for f in $presence_list; do echo "    - $f"; done
fi
if [ -n "$both_no_output_list" ]; then
  echo "  双方均无输出（不可比对）:"
  for f in $both_no_output_list; do echo "    - $f"; done
fi

# ---- 4) 确定性项目 fixture（单包 module；严格） --------------------------------
echo "[4/5] 确定性项目 fixture 对比（单包 module，忽略 tmpN）"
cat > "$FIXTURE/go.mod" <<'EOF'
module fixture

go 1.22
EOF
cat > "$FIXTURE/a.go" <<'EOF'
package fixture

type Point struct {
	X int
	Y int
}

func A() Point { return Point{X: 1, Y: 2} }

func Sum(p Point) int { return p.X + p.Y }
EOF
cat > "$FIXTURE/b.go" <<'EOF'
package fixture

func B(s string) string { return s + "!" }
EOF

rm -f "$OUT/o-fix.json" "$OUT/c-fix.json"
fix_rc_o=0
"$OUT/uast4go-original" -rootDir="$FIXTURE" -output="$OUT/o-fix.json" >"$OUT/o-fix.log" 2>"$OUT/o-fix.err" || fix_rc_o=$?
fix_rc_c=0
"$OUT/uast4go-current" -rootDir="$FIXTURE" -output="$OUT/c-fix.json" >"$OUT/c-fix.log" 2>"$OUT/c-fix.err" || fix_rc_c=$?
fix_no_output=0
fixture_diff=0
if [ ! -s "$OUT/o-fix.json" ] || [ ! -s "$OUT/c-fix.json" ]; then
  fix_no_output=1
  echo "  [no-output] fixture (orig_rc=$fix_rc_o current_rc=$fix_rc_c)"
elif sed 's/tmp[0-9]*/tmpX/g' "$OUT/o-fix.json" > "$OUT/o-fix.norm" \
  && sed 's/tmp[0-9]*/tmpX/g' "$OUT/c-fix.json" > "$OUT/c-fix.norm" \
  && cmp -s "$OUT/o-fix.norm" "$OUT/c-fix.norm"; then
  echo "  一致"
else
  fixture_diff=1
  echo "  [diff] fixture 输出有差异（已忽略 tmpN）"
fi

# ---- 5) examples 项目模式（信息性） --------------------------------------------
echo "[5/5] 项目模式 -rootDir=examples（信息性，不计入退出码）"
rm -f "$OUT/o-proj.json" "$OUT/c-proj.json"
proj_rc_o=0
"$OUT/uast4go-original" -rootDir=examples -output="$OUT/o-proj.json" >"$OUT/o-proj.log" 2>"$OUT/o-proj.err" || proj_rc_o=$?
proj_rc_c=0
"$OUT/uast4go-current" -rootDir=examples -output="$OUT/c-proj.json" >"$OUT/c-proj.log" 2>"$OUT/c-proj.err" || proj_rc_c=$?
if [ ! -s "$OUT/o-proj.json" ] || [ ! -s "$OUT/c-proj.json" ]; then
  echo "  [no-output] examples 项目模式 (orig_rc=$proj_rc_o current_rc=$proj_rc_c)"
elif sed 's/tmp[0-9]*/tmpX/g' "$OUT/o-proj.json" > "$OUT/o-proj.norm" \
  && sed 's/tmp[0-9]*/tmpX/g' "$OUT/c-proj.json" > "$OUT/c-proj.norm" \
  && cmp -s "$OUT/o-proj.norm" "$OUT/c-proj.norm"; then
  echo "  一致"
else
  echo "  有差异 —— 原版既有非确定性：目录内随机选包（main.go:27-29，本次不修，见 wasm-plan §5.4）"
  echo "  + 跨文件 map 顺序（builder.go:101 遍历 pkg.Files，影响 tmpN 编号）。"
  echo "  项目模式严格对比不可用，以单文件逐字节 + 确定性 fixture + Engine benchmark 为准。"
fi

# ---- wasm 单文件（严格，可选） --------------------------------------------------
wasm_total=0
wasm_match=0
wasm_diff=0
wasm_presence=0
if [ "$WITH_WASM" -eq 1 ]; then
  echo "[+wasm] 构建当前 wasm 并对比单文件"
  ( cd "$PG" && GOOS=js GOARCH=wasm go build -buildvcs=false -o "$OUT/uast4go-current.wasm" . )
  WEXEC="$GOROOT/misc/wasm/wasm_exec_node.js"
  [ -f "$WEXEC" ] || WEXEC="$GOROOT/lib/wasm/wasm_exec_node.js"
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    wasm_total=$((wasm_total + 1))
    rm -f "$OUT/o.json" "$OUT/c-wasm.json"
    o_rc=0
    "$OUT/uast4go-original" -single -rootDir="$f" -output="$OUT/o.json" >/dev/null 2>&1 || o_rc=$?
    w_rc=0
    node "$WEXEC" "$OUT/uast4go-current.wasm" -single -rootDir="$f" -output="$OUT/c-wasm.json" >/dev/null 2>&1 || w_rc=$?
    o_ok=1; [ -s "$OUT/o.json" ] || o_ok=0
    w_ok=1; [ -s "$OUT/c-wasm.json" ] || w_ok=0
    if [ "$o_ok" -eq 0 ] && [ "$w_ok" -eq 0 ]; then
      echo "  [both-no-output] $f (orig_rc=$o_rc wasm_rc=$w_rc)"
    elif [ "$o_ok" -eq 0 ] || [ "$w_ok" -eq 0 ]; then
      wasm_presence=$((wasm_presence + 1))
      echo "  [no-output] $f (orig_rc=$o_rc orig_out=$o_ok wasm_rc=$w_rc wasm_out=$w_ok)"
    elif cmp -s "$OUT/o.json" "$OUT/c-wasm.json"; then
      wasm_match=$((wasm_match + 1))
    else
      wasm_diff=$((wasm_diff + 1))
      echo "  [diff] $f"
    fi
  done <<< "$files"
  echo "  wasm 单文件: $wasm_match/$wasm_total 一致 (差异=$wasm_diff, 一方无输出=$wasm_presence)"
fi

# ---- 汇总 ---------------------------------------------------------------------
strict_fail=$((single_diff + single_presence + fix_no_output + fixture_diff))
if [ "$WITH_WASM" -eq 1 ]; then
  strict_fail=$((strict_fail + wasm_diff + wasm_presence))
fi

echo
if [ "$strict_fail" -eq 0 ]; then
  echo "RESULT: PASS (单文件逐字节 + 确定性项目 fixture 严格对比通过；examples 项目模式为信息性)"
  exit 0
fi
echo "RESULT: FAIL (严格对比存在差异：单文件输出差异=$single_diff, 单文件一方无输出=$single_presence, fixture 无输出=$fix_no_output, fixture 输出差异=$fixture_diff; wasm 差异=$wasm_diff, wasm 一方无输出=$wasm_presence)"
exit 1
