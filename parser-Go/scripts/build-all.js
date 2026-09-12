'use strict'

// 统一构建编排，供 prepare / pretest / prepack 复用：
//   node scripts/build-all.js           # prepare：产物齐全则跳过；缺失则构建（无 go 报错）
//   node scripts/build-all.js --force   # prepack/pretest：总是构建 dist + dist-wasm
//
// 所有子命令的 stdout 重定向到 stderr，保证 `npm pack --json` / `npm publish` 的
// stdout 只含 npm 自己的 JSON，便于稳定解析（F7）。

const { spawnSync } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const ROOT = path.join(__dirname, '..')
const FORCE = process.argv.includes('--force')
const ARTIFACTS = [
  path.join(ROOT, 'dist', 'src', 'index.js'),
  path.join(ROOT, 'dist-wasm', 'uast4go.wasm'),
  path.join(ROOT, 'dist-wasm', 'wasm_exec.js'),
]
const npmCmd = process.platform === 'win32' ? 'npm.cmd' : 'npm'

const log = (msg) => console.error(`[build-all] ${msg}`)
const haveAll = ARTIFACTS.every((p) => fs.existsSync(p))

function hasGo() {
  const r = spawnSync('go', ['version'], { stdio: 'ignore' })
  return r.status === 0
}

function runScript(script) {
  // stdout（fd 1）→ 父进程 stderr（fd 2）；stderr 原样继承。
  const r = spawnSync(npmCmd, ['run', script], {
    cwd: ROOT,
    stdio: ['ignore', 2, 'inherit'],
  })
  if (r.status !== 0) {
    log(`失败: npm run ${script}（exit ${r.status}）`)
    process.exit(r.status || 1)
  }
}

if (!FORCE && haveAll) {
  log('产物已存在，跳过构建（prepare）')
  process.exit(0)
}

if (!hasGo()) {
  log('错误: PATH 上未找到 go，无法构建 dist-wasm（请安装 Go 或提供已构建产物）')
  process.exit(1)
}

log(FORCE ? '强制构建 dist + dist-wasm（prepack/pretest）' : '构建 dist + dist-wasm（prepare）')
runScript('build')
runScript('build:wasm')
log('构建完成')
