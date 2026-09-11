'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')

const { Parser } = require('../dist/src/index.js')

test('an export throw marks the instance not-ready and a later init recovers', async () => {
  // 拦截 instantiate，把首个实例的 uast_parse 换成会抛错的导出，模拟 wasm trap。
  const realInstantiate = WebAssembly.instantiate
  let armed = true
  WebAssembly.instantiate = async (bytes, importObject) => {
    const result = await realInstantiate(bytes, importObject)
    if (armed) {
      const ex = result.instance.exports
      const wrapped = Object.create(null)
      for (const k of Object.keys(ex)) wrapped[k] = ex[k]
      wrapped.uast_parse = () => {
        throw new WebAssembly.RuntimeError('boom')
      }
      Object.defineProperty(result.instance, 'exports', { value: wrapped, configurable: true })
    }
    return result
  }

  try {
    const parser = new Parser()
    await parser.init()
    assert.throws(() => parser.parseSource('x.go', 'package p\n\nfunc F() {}\n'), /boom/)

    // loader 已丢弃中毒实例；重新 init 必须能重建并正常工作。
    armed = false
    const recovered = new Parser()
    await recovered.init()
    const obj = recovered.parseSource('x.go', 'package p\n\nfunc F() {}\n')
    assert.ok(obj && obj.packageInfo, 'parse must work after re-init')
  } finally {
    WebAssembly.instantiate = realInstantiate
  }
})
