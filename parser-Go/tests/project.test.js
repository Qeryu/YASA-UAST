'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const { runCLI, normalizeTmpN } = require('./helpers')
const { Parser } = require('../dist/src/index.js')

test('parseProject matches -rootDir for a single-package fixture (tmpN normalized)', async () => {
  const parser = new Parser()
  await parser.init()

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'uast4go-proj-'))
  const goMod = 'module fixture\n\ngo 1.22\n'
  const aGo = [
    'package fixture',
    '',
    'type Point struct {',
    '\tX int',
    '\tY int',
    '}',
    '',
    'func A() Point { return Point{X: 1, Y: 2} }',
    '',
  ].join('\n')
  const bGo = 'package fixture\n\nfunc B(s string) string { return s + "!" }\n'
  fs.writeFileSync(path.join(dir, 'go.mod'), goMod)
  fs.writeFileSync(path.join(dir, 'a.go'), aGo)
  fs.writeFileSync(path.join(dir, 'b.go'), bGo)

  const out = path.join(dir, 'out.json')
  runCLI([`-rootDir=${dir}`, `-output=${out}`])
  const cli = fs.readFileSync(out, 'utf8')

  const obj = parser.parseProject([
    { name: path.join(dir, 'go.mod'), content: goMod },
    { name: path.join(dir, 'a.go'), content: aGo },
    { name: path.join(dir, 'b.go'), content: bGo },
  ])

  assert.deepEqual(
    JSON.parse(normalizeTmpN(JSON.stringify(obj))),
    JSON.parse(normalizeTmpN(cli))
  )
  assert.equal(parser.lastErrors.length, 0)
})
