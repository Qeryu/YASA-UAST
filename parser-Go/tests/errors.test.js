'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const { readExample } = require('./helpers')
const { Parser } = require('../dist/src/index.js')

test('bad single source throws without crashing; instance stays usable', async () => {
  const parser = new Parser()
  await parser.init()

  assert.throws(
    () => parser.parseSource('bad.go', 'package p\n\nfunc broken( {\n'),
    /uast 解析失败/
  )
  assert.ok(
    parser.lastErrors.some((e) => e.severity === 'error' && e.kind === 'parse_error'),
    `expected error/parse_error in lastErrors, got ${JSON.stringify(parser.lastErrors)}`
  )

  // 常驻实例必须在一次解析失败后依然可用。
  const ok = parser.parseSource('examples/imports.go', readExample('examples/imports.go'))
  assert.ok(ok && ok.packageInfo, 'instance must remain usable after a failed parse')
})

test('project partial failure returns data and reports error/parse_error', async () => {
  const parser = new Parser()
  await parser.init()

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'uast4go-err-'))
  const goMod = 'module fixture\n\ngo 1.22\n'
  const good = 'package fixture\n\nfunc Good() int { return 1 }\n'
  const bad = 'package fixture\n\nfunc broken( {\n'
  fs.writeFileSync(path.join(dir, 'go.mod'), goMod)
  fs.writeFileSync(path.join(dir, 'good.go'), good)
  fs.writeFileSync(path.join(dir, 'bad.go'), bad)

  const obj = parser.parseProject([
    { name: path.join(dir, 'go.mod'), content: goMod },
    { name: path.join(dir, 'good.go'), content: good },
    { name: path.join(dir, 'bad.go'), content: bad },
  ])

  assert.ok(obj && obj.packageInfo, 'good files must still be emitted')
  assert.ok(
    parser.lastErrors.some((e) => e.severity === 'error' && e.kind === 'parse_error'),
    `expected error/parse_error in lastErrors, got ${JSON.stringify(parser.lastErrors)}`
  )
})

test('project without go.mod surfaces warning/no_gomod', async () => {
  const parser = new Parser()
  await parser.init()

  const obj = parser.parseProject([{ name: 'a.go', content: 'package p\n\nfunc A() {}\n' }])
  assert.ok(obj && obj.packageInfo, 'in-memory project must still be built')
  assert.ok(
    parser.lastErrors.some((e) => e.severity === 'warning' && e.kind === 'no_gomod'),
    `expected warning/no_gomod in lastErrors, got ${JSON.stringify(parser.lastErrors)}`
  )
})
