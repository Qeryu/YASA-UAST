'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const { ROOT, runCLI, readExample } = require('./helpers')
const { Parser } = require('../dist/src/index.js')

// >= 6 examples covering composite literals, methods, imports, promotion,
// assertions and select.
const EXAMPLES = [
  'examples/compositeLit.go',
  'examples/method.go',
  'examples/imports.go',
  'examples/struct_promotion.go',
  'examples/typeAsserts.go',
  'examples/select.go',
]

test('parseSourceRaw with an absolute path matches -single CLI (Engine uses abs paths)', async () => {
  const parser = new Parser()
  await parser.init()

  const rel = 'examples/method.go'
  const abs = path.join(ROOT, rel)
  const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'uast4go-abs-')), 'out.json')

  runCLI(['-single', `-rootDir=${abs}`, `-output=${out}`])
  const cli = fs.readFileSync(out, 'utf8')
  const raw = parser.parseSourceRaw(abs, fs.readFileSync(abs, 'utf8'))

  assert.equal(raw, cli, 'absolute-path output must match -single CLI')
  // The loc.sourcefile must carry the absolute path verbatim.
  assert.ok(JSON.parse(raw).packageInfo.subs['/'].files[abs], 'sourcefile key should be the absolute path')
})

test('parseSourceRaw is byte-identical to -single CLI on >=6 examples', async () => {
  assert.ok(EXAMPLES.length >= 6, 'need >= 6 examples')
  const parser = new Parser()
  await parser.init()

  const outDir = fs.mkdtempSync(path.join(os.tmpdir(), 'uast4go-parity-'))
  for (const rel of EXAMPLES) {
    const out = path.join(outDir, rel.replace(/[\\/]/g, '_') + '.json')
    runCLI(['-single', `-rootDir=${rel}`, `-output=${out}`])
    const cli = fs.readFileSync(out, 'utf8')
    const raw = parser.parseSourceRaw(rel, readExample(rel))
    assert.equal(raw, cli, `byte mismatch for ${rel}`)
    assert.equal(parser.lastErrors.length, 0, `unexpected errors for ${rel}`)
  }
})
