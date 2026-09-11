'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')

const { Parser, LanguageType, version } = require('../dist/src/index.js')

test('LanguageType.LANG_GO matches the Engine language string', () => {
  assert.equal(LanguageType.LANG_GO, 'golang')
  assert.equal(LanguageType.OTHER, 'other')
})

test('version is exported from package.json', () => {
  assert.match(version, /^\d+\.\d+\.\d+/)
})

test('constructor accepts (and ignores) opts; parse() aliases parseSource', async () => {
  const parser = new Parser({ ignored: true })
  await parser.init()
  const code = 'package p\n\nfunc F() {}\n'

  const viaParse = parser.parse(code, { sourcefile: 'via-parse.go' })
  assert.ok(viaParse && viaParse.packageInfo)
  assert.ok(
    viaParse.packageInfo.subs['/'].files['via-parse.go'],
    'opts.sourcefile should be used as loc.sourcefile'
  )

  const viaSource = parser.parseSource('via-parse.go', code)
  assert.deepEqual(viaParse, viaSource, 'parse() must alias parseSource()')

  // Omitting opts falls back to '' as sourcefile without throwing.
  assert.ok(parser.parse(code).packageInfo)
})
