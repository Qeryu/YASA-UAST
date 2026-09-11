'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const { execFileSync } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const { ROOT } = require('./helpers')

const pkg = require('../package.json')

test('package.json wires build/build:wasm/prepack and stays runtime-dependency free', () => {
  assert.equal(pkg.name, '@ant-yasa/uast-parser-go')
  assert.equal(pkg.main, 'dist/src/index.js')
  assert.equal(pkg.types, 'dist/src/index.d.ts')
  assert.ok(!pkg.dependencies || Object.keys(pkg.dependencies).length === 0, 'zero runtime dependencies')
  assert.match(pkg.scripts.build, /tsc/)
  assert.match(pkg.scripts['build:wasm'], /build-wasm/)
  assert.match(pkg.scripts.prepack, /build/)
  assert.ok(pkg.files.includes('dist/') && pkg.files.includes('dist-wasm/'))
})

test('npm pack --dry-run lists dist/ and dist-wasm/ assets', () => {
  // Ensure the wasm assets exist (verification runs build:wasm before test, but
  // be self-sufficient if the test is run alone).
  if (!fs.existsSync(path.join(ROOT, 'dist-wasm', 'uast4go.wasm'))) {
    execFileSync('npm', ['run', 'build:wasm'], { cwd: ROOT, stdio: 'pipe' })
  }

  const out = execFileSync('npm', ['pack', '--dry-run', '--json', '--ignore-scripts'], {
    cwd: ROOT,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const parsed = JSON.parse(out)
  const entry = Array.isArray(parsed) ? parsed[0] : parsed
  const files = entry.files.map((f) => f.path)

  assert.ok(files.includes('dist/src/index.js'), `dist/src/index.js missing: ${files.join(', ')}`)
  assert.ok(files.includes('dist/src/index.d.ts'), 'dist/src/index.d.ts missing')
  assert.ok(files.includes('dist-wasm/uast4go.wasm'), 'dist-wasm/uast4go.wasm missing')
  assert.ok(files.includes('dist-wasm/wasm_exec.js'), 'dist-wasm/wasm_exec.js missing')
  assert.ok(!files.some((f) => f.startsWith('src/')), 'src/ must not be published')
})

test('a fresh process can parse from dist', () => {
  const out = execFileSync(process.execPath, [path.join(ROOT, 'tests', 'new-process-parse.js')], {
    cwd: ROOT,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  assert.match(out, /PACK_PARSE_OK/)
})
