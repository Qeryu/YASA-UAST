'use strict'

// Spawned by pack.test.js in a fresh Node process to prove a packed/dist
// install can parse once. Not a test file itself (does not match *.test.js).

const { Parser } = require('../dist/src/index.js')

;(async () => {
  const parser = new Parser()
  await parser.init()
  const obj = parser.parseSource('x.go', 'package p\n\nfunc F() {}\n')
  if (!obj || !obj.packageInfo) {
    throw new Error('expected packageInfo in parse result')
  }
  console.log('PACK_PARSE_OK')
})().catch((err) => {
  console.error(err)
  process.exit(1)
})
