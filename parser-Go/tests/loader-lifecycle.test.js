'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')

const { Parser } = require('../dist/src/index.js')

test('an export throw marks the instance not-ready and a later init recovers', async () => {
  const parser = new Parser()
  await parser.init()

  // 模拟注册 export 上发生一次 wasm trap。
  globalThis.__uastGoParse = () => {
    throw new Error('boom')
  }
  assert.throws(() => parser.parseSource('x.go', 'package p\n\nfunc F() {}\n'), /boom/)

  // loader 已丢弃中毒实例；重新 init 必须能重建并正常工作。
  const recovered = new Parser()
  await recovered.init()
  const obj = recovered.parseSource('x.go', 'package p\n\nfunc F() {}\n')
  assert.ok(obj && obj.packageInfo, 'parse must work after re-init')
})
