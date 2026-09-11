import * as fs from 'fs'
import * as path from 'path'

/**
 * Go parser 报告的 file-level 错误项（对应 uast.ParseError）。
 */
export interface ParseError {
  file: string
  message: string
  severity: 'warning' | 'error'
  kind: string
}

/** 常驻 wasm 返回的原始响应 envelope。 */
export interface GoResponse {
  /** 协议版本（loader/handler 内部 envelope）。 */
  v?: number
  ok: boolean
  /** 与 CLI 等价的 JSON 字符串（ok 为 true 时存在）。 */
  data?: string
  errors?: ParseError[]
}

/** 内部协议版本；请求/响应形状变更时递增。 */
export const PROTOCOL_VERSION = 1

/** 随包分发的 wasm 二进制绝对路径。 */
const WASM_BIN = path.join(__dirname, '..', '..', 'dist-wasm', 'uast4go.wasm')

let ready = false
let initPromise: Promise<void> | null = null
let instance: WebAssembly.Instance | null = null

/**
 * 准备 wasm_exec.js 依赖的全局变量，顺序与官方 misc/wasm/wasm_exec_node.js
 * 一致。需在加载 wasm_exec.js 之前完成，因为其 IIFE 会检查（并可能兜底）
 * 这些全局变量。
 */
function defineIfMissing(name: string, value: unknown): void {
  const g = globalThis as unknown as Record<string, unknown>
  if (g[name] === undefined) {
    // 用 defineProperty 规避 Node 只读访问器全局变量（如 Node 22 的
    // globalThis.crypto / performance）赋值报错。
    Object.defineProperty(g, name, { value, writable: true, configurable: true, enumerable: true })
  }
}

function prepareGlobals(): void {
  defineIfMissing('require', require)
  defineIfMissing('fs', require('fs'))
  defineIfMissing('path', require('path'))
  defineIfMissing('TextEncoder', require('util').TextEncoder)
  defineIfMissing('TextDecoder', require('util').TextDecoder)
  defineIfMissing('performance', require('perf_hooks').performance)
  defineIfMissing('crypto', require('crypto'))
}

/**
 * //go:wasmexport 导出在本实例的线性内存上交换数据，因此直接从
 * instance.exports 取函数与 mem。
 */
interface WasmExports {
  mem: WebAssembly.Memory
  uast_alloc(size: number): number
  uast_parse(ptr: number, len: number): bigint
}

function exportsOf(inst: WebAssembly.Instance): WasmExports {
  return inst.exports as unknown as WasmExports
}

/**
 * 等待 Go runtime 就绪：导出符号存在后，用一次空分配探针确认 runtime 已初始化
 * （未初始化时调用导出可能抛错）。
 */
async function waitForExports(inst: WebAssembly.Instance, timeoutMs = 5000): Promise<void> {
  const started = Date.now()
  for (;;) {
    const ex = exportsOf(inst)
    if (typeof ex.uast_alloc === 'function' && typeof ex.uast_parse === 'function' && ex.mem) {
      try {
        ex.uast_alloc(0)
        return
      } catch {
        /* runtime 未就绪，继续重试 */
      }
    }
    if (Date.now() - started > timeoutMs) {
      throw new Error(`wasm 导出 uast_alloc/uast_parse 在 ${timeoutMs}ms 内未就绪`)
    }
    await new Promise((resolve) => setImmediate(resolve))
  }
}

/** 丢弃常驻实例，使后续 init() 重建一个全新实例。 */
function resetInstance(): void {
  ready = false
  initPromise = null
  instance = null
}

/**
 * 加载并启动常驻 wasm 实例。成功期间幂等；init 失败会重置单例，调用方可重试。
 */
export async function init(): Promise<void> {
  if (ready) return
  if (initPromise) return initPromise

  initPromise = (async () => {
    prepareGlobals()

    // pkg 兼容：使用静态字符串字面量，pkg 的静态分析才能在构建期发现该资产。
    // 运行时拼接路径（path.join(...)）对 pkg 静态分析不可见。
    require('../../dist-wasm/wasm_exec.js')

    const GoCtor = (globalThis as unknown as Record<string, unknown>).Go
    if (typeof GoCtor !== 'function') {
      throw new Error('dist-wasm/wasm_exec.js 未定义 globalThis.Go')
    }
    if (!fs.existsSync(WASM_BIN)) {
      throw new Error(`wasm 文件不存在: ${WASM_BIN}（请运行 "npm run build:wasm"）`)
    }

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const go = new (GoCtor as any)()
    // 绝不杀死宿主进程：Go 不可恢复退出应由 loader 处理（实例可丢弃重建），
    // 而不是 process.exit。
    go.exit = (_code: number): void => {
      /* no-op */
    }
    // argv[0] 必须是程序名占位，否则参数处理会错位。
    go.argv = ['uast4go.wasm']
    // 最小 env：parser 不读任何环境变量，且透传整个 process.env 可能撑爆 wasm
    // 固定的 argv/env 缓冲。
    go.env = { TMPDIR: require('os').tmpdir() }

    const bytes = fs.readFileSync(WASM_BIN)
    const wasmInstance = (await WebAssembly.instantiate(bytes, go.importObject)).instance
    // 常驻：Go main 阻塞在 select{}，切勿 await run()（它永不 settle）。
    void go.run(wasmInstance)
    instance = wasmInstance

    await waitForExports(wasmInstance)
    ready = true
  })().catch((err) => {
    // init 失败不能污染单例：允许干净重试。
    resetInstance()
    throw err
  })

  return initPromise
}

export function isReady(): boolean {
  return ready
}

/**
 * 同步调用常驻 wasm parser（//go:wasmexport 运输层）：
 * 1. uast_alloc(len) 分配请求缓冲并取得指针；
 * 2. 把请求字节写入线性内存 [ptr, ptr+len)；
 * 3. uast_parse(ptr, len) 返回打包结果 (resultPtr<<32)|resultLen；
 * 4. 从线性内存读回响应 JSON。
 *
 * 调用天然串行（同步导出，JS 无法交错执行）。`ok:false` 不会在此抛出——由
 * Parser 映射为 JS Error 并保留 `errors` 可见。导出抛错（如 wasm trap）会丢弃
 * 实例，后续 init() 可重建。
 */
export function call(request: unknown): GoResponse {
  if (!ready || !instance) {
    throw new Error('Parser 未初始化，请先 await parser.init()')
  }

  const ex = exportsOf(instance)
  const payload = new TextEncoder().encode(
    JSON.stringify(Object.assign({ v: PROTOCOL_VERSION }, request as Record<string, unknown>))
  )

  let raw: string
  try {
    const ptr = ex.uast_alloc(payload.length)
    if (payload.length > 0) {
      // alloc 可能增长过内存，写入前重新取 buffer。
      new Uint8Array(ex.mem.buffer, ptr, payload.length).set(payload)
    }
    const packed = BigInt.asUintN(64, ex.uast_parse(ptr, payload.length))
    const resultPtr = Number(packed >> 32n)
    const resultLen = Number(packed & 0xffffffffn)
    // parse 可能增长过内存，读回前重新取 buffer。
    raw = new TextDecoder().decode(new Uint8Array(ex.mem.buffer, resultPtr, resultLen))
  } catch (e) {
    resetInstance()
    throw e
  }

  let resp: GoResponse
  try {
    resp = JSON.parse(raw) as GoResponse
  } catch (e) {
    resetInstance()
    throw new Error(`wasm 响应不是合法 JSON: ${String(e)}`)
  }

  if (resp.v !== PROTOCOL_VERSION) {
    resetInstance()
    throw new Error(`协议版本不匹配：期望 ${PROTOCOL_VERSION}，实际 ${String(resp.v)}`)
  }
  return resp
}
