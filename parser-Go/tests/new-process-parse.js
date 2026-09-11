'use strict'

// 由 pack.test.js 在新 Node 进程中拉起，用于证明打包/dist 安装能成功解析一次。
// 它本身不是测试文件（不匹配 *.test.js）。

const { Parser } = require('../dist/src/index.js')

;(async () => {
  const parser = new Parser()
  await parser.init()
  const obj = parser.parseSource('x.go', 'package p\n\nfunc F() {}\n')
  if (!obj || !obj.packageInfo) {
    throw new Error('解析结果中缺少 packageInfo')
  }
  console.log('PACK_PARSE_OK')
})().catch((err) => {
  console.error(err)
  process.exit(1)
})
