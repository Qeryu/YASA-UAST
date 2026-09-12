'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')

const { ROOT } = require('./helpers')
const { Parser } = require('../dist/src/index.js')

// ── 归一化：逐行复刻 parser-Go/uast_test.go:115-205 的
//    normalizeJSONForGoldenCompare / normalizeMapForGolden /
//    normalizeExamplePath / isNumberZero / isReceiveClsOnlyMeta ──

function normalizeExamplePath(value) {
  if (!value.includes('examples') || !value.endsWith('.go')) return null
  const v = value.replace(/\\/g, '/')
  const i = v.lastIndexOf('examples/')
  if (i >= 0) return v.slice(i)
  return null
}

function isNumberZero(v) {
  return typeof v === 'number' && v === 0
}

function isReceiveClsOnlyMeta(v) {
  if (v === null || typeof v !== 'object' || Array.isArray(v)) return false
  if (Object.keys(v).length !== 1) return false
  if (!Object.prototype.hasOwnProperty.call(v, 'ReceiveCls')) return false
  const rc = v.ReceiveCls
  return rc === '' || rc === null
}

function normalizeMapForGolden(m) {
  const keyRenames = []
  for (const k of Object.keys(m)) {
    const v = m[k]
    if (k === 'Offset') {
      if (isNumberZero(v)) delete m[k]
      continue
    }
    if (k === 'goModPath' || k === 'numOfGoMod') {
      delete m[k]
      continue
    }
    if (k === '_meta') {
      if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
        normalizeMapForGolden(v)
        if (isReceiveClsOnlyMeta(v)) m[k] = {}
      }
      continue
    }
    // 路径有时在 key 里（如 files 的 key 是文件路径）
    const renamed = normalizeExamplePath(k)
    if (renamed !== null && renamed !== k) keyRenames.push([k, renamed])
    if (typeof v === 'string') {
      const nv = normalizeExamplePath(v)
      if (nv !== null) m[k] = nv
    } else if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
      normalizeMapForGolden(v)
    } else if (Array.isArray(v)) {
      for (const e of v) {
        if (e !== null && typeof e === 'object' && !Array.isArray(e)) normalizeMapForGolden(e)
      }
    }
  }
  for (const [oldK, newK] of keyRenames) {
    m[newK] = m[oldK]
    delete m[oldK]
  }
}

function normalizeJSONForGoldenCompare(jsonStr) {
  const m = JSON.parse(jsonStr)
  normalizeMapForGolden(m)
  return m
}

// 递归定位首个差异路径（仅用于失败信息，不参与判定）。
function firstDiff(actual, expected, p = '$') {
  if (actual === expected) return null
  if (typeof actual !== typeof expected) return `${p}: type ${typeof actual} vs ${typeof expected}`
  if (actual === null || expected === null) return `${p}: ${JSON.stringify(actual)} vs ${JSON.stringify(expected)}`
  if (Array.isArray(actual) || Array.isArray(expected)) {
    if (!Array.isArray(actual) || !Array.isArray(expected)) return `${p}: array vs non-array`
    if (actual.length !== expected.length) return `${p}.length: ${actual.length} vs ${expected.length}`
    for (let i = 0; i < actual.length; i++) {
      const d = firstDiff(actual[i], expected[i], `${p}[${i}]`)
      if (d) return d
    }
    return null
  }
  if (typeof actual === 'object') {
    for (const k of Object.keys(actual)) {
      if (!Object.prototype.hasOwnProperty.call(expected, k)) return `${p}.${k}: present in actual only`
      const d = firstDiff(actual[k], expected[k], `${p}.${k}`)
      if (d) return d
    }
    for (const k of Object.keys(expected)) {
      if (!Object.prototype.hasOwnProperty.call(actual, k)) return `${p}.${k}: present in expected only`
    }
    return null
  }
  return `${p}: ${JSON.stringify(actual)} vs ${JSON.stringify(expected)}`
}

function listExamples(dir) {
  const out = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) out.push(...listExamples(full))
    else if (entry.name.endsWith('.go')) out.push(full)
  }
  return out.sort()
}

test('golden: npm 包对 examples/**/*.go.json 归一化后 deep-equal', async () => {
  const parser = new Parser()
  await parser.init()

  const goFiles = listExamples(path.join(ROOT, 'examples'))
  assert.ok(goFiles.length >= 60, `examples 用例过少: ${goFiles.length}`)

  const failures = []
  let passed = 0
  let total = 0

  for (const abs of goFiles) {
    const rel = path.relative(ROOT, abs).split(path.sep).join('/')
    const goldenPath = abs + '.json'
    total++
    if (!fs.existsSync(goldenPath)) {
      failures.push({ rel, detail: 'golden .go.json 不存在' })
      continue
    }

    const actualRaw = parser.parseSourceRaw(rel, fs.readFileSync(abs, 'utf8'))
    const actual = normalizeJSONForGoldenCompare(actualRaw)
    const expected = normalizeJSONForGoldenCompare(fs.readFileSync(goldenPath, 'utf8'))

    try {
      assert.deepStrictEqual(actual, expected, `golden mismatch: ${rel}`)
      passed++
    } catch (e) {
      const d = firstDiff(actual, expected)
      failures.push({
        rel,
        detail: `first diff: ${d}\n${String(e.message).slice(0, 3000)}`,
      })
    }
  }

  // 单例批量场景的冒烟：另取 1 个例子走 parseSource（对象）。
  const smokeRel = path.relative(ROOT, goFiles[0]).split(path.sep).join('/')
  const smokeRaw = parser.parseSourceRaw(smokeRel, fs.readFileSync(goFiles[0], 'utf8'))
  const smokeObj = parser.parseSource(smokeRel, fs.readFileSync(goFiles[0], 'utf8'))
  assert.ok(smokeObj && smokeObj.packageInfo, `parseSource 冒烟失败: ${smokeRel}`)
  assert.deepStrictEqual(smokeObj, JSON.parse(smokeRaw), `parseSource 对象与 parseSourceRaw 不一致: ${smokeRel}`)

  console.log(`[golden] passed=${passed} failed=${failures.length} total=${total}`)

  if (failures.length > 0) {
    const lines = failures.map((f) => `  - ${f.rel}\n    ${f.detail}`)
    assert.fail(`${failures.length}/${total} 个 golden 不匹配（未修改 golden、未放宽断言）:\n${lines.join('\n')}`)
  }
})
