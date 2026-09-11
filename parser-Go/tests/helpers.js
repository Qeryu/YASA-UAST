'use strict'

const { execFileSync } = require('node:child_process')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const ROOT = path.join(__dirname, '..')

let cachedCLI = null

/** Build the native CLI once (used as the parity oracle) into a temp dir. */
function buildCLI() {
  if (cachedCLI) return cachedCLI
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'uast4go-cli-'))
  const bin = path.join(dir, 'uast4go')
  execFileSync('go', ['build', '-buildvcs=false', '-o', bin, '.'], {
    cwd: ROOT,
    env: Object.assign({}, process.env, { CGO_ENABLED: '0' }),
    stdio: 'pipe',
  })
  cachedCLI = bin
  return bin
}

function runCLI(args, cwd = ROOT) {
  execFileSync(buildCLI(), args, { cwd, stdio: 'pipe' })
}

/** Normalize the builder's per-package tmpN counter for cross-run comparison. */
function normalizeTmpN(s) {
  return s.replace(/tmp[0-9]+/g, 'tmpX')
}

function readExample(rel) {
  return fs.readFileSync(path.join(ROOT, rel), 'utf8')
}

module.exports = { ROOT, buildCLI, runCLI, normalizeTmpN, readExample }
