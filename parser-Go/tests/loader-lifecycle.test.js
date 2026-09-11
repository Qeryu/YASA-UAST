'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')

const { Parser } = require('../dist/src/index.js')

test('an export throw marks the instance not-ready and a later init recovers', async () => {
  const parser = new Parser()
  await parser.init()

  // Simulate a wasm trap on the registered export.
  globalThis.__uastGoParse = () => {
    throw new Error('boom')
  }
  assert.throws(() => parser.parseSource('x.go', 'package p\n\nfunc F() {}\n'), /boom/)

  // The loader dropped the poisoned instance; a fresh init must rebuild and work.
  const recovered = new Parser()
  await recovered.init()
  const obj = recovered.parseSource('x.go', 'package p\n\nfunc F() {}\n')
  assert.ok(obj && obj.packageInfo, 'parse must work after re-init')
})
