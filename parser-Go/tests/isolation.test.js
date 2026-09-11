'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')

const { readExample } = require('./helpers')
const { Parser } = require('../dist/src/index.js')

test('repeated calls are deterministic and A->B->A has no state leak', async () => {
  const parser = new Parser()
  await parser.init()

  const nameA = 'examples/method.go'
  const nameB = 'examples/imports.go'
  const codeA = readExample(nameA)
  const codeB = readExample(nameB)

  const a1 = parser.parseSourceRaw(nameA, codeA)
  const a1Again = parser.parseSourceRaw(nameA, codeA)
  const b = parser.parseSourceRaw(nameB, codeB)
  const a2 = parser.parseSourceRaw(nameA, codeA)

  assert.equal(a1, a1Again, 'same input must produce identical output')
  assert.equal(a1, a2, 'A -> B -> A must not leak state')
  assert.notEqual(a1, b, 'different inputs should differ')
})
