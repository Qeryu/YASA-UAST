import * as fs from 'fs'
import * as path from 'path'

/**
 * A file-level entry reported by the Go parser (mirrors uast.ParseError).
 */
export interface ParseError {
  file: string
  message: string
  severity: 'warning' | 'error'
  kind: string
}

/** Raw response envelope produced by the resident wasm handler. */
export interface GoResponse {
  /** Protocol version (internal loader/handler envelope). */
  v?: number
  ok: boolean
  /** CLI-equivalent JSON string (present iff ok). */
  data?: string
  errors?: ParseError[]
}

/** Internal protocol version; bump when the request/response shape changes. */
export const PROTOCOL_VERSION = 1

/** Absolute path of the wasm binary shipped inside the package. */
const WASM_BIN = path.join(__dirname, '..', '..', 'dist-wasm', 'uast4go.wasm')

const EXPORT_NAME = '__uastGoParse'

let ready = false
let initPromise: Promise<void> | null = null

/**
 * Prepare the globals that wasm_exec.js expects, in the same order as the
 * official misc/wasm/wasm_exec_node.js runner. Done before loading wasm_exec.js
 * because its IIFE checks for (and may otherwise polyfill) these globals.
 */
function defineIfMissing(name: string, value: unknown): void {
  const g = globalThis as unknown as Record<string, unknown>
  if (g[name] === undefined) {
    // defineProperty avoids "only a getter" failures for Node's accessor globals
    // (e.g. globalThis.crypto / performance on Node 22).
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

async function waitForExport(name: string, timeoutMs = 5000): Promise<void> {
  const started = Date.now()
  while (typeof (globalThis as Record<string, unknown>)[name] !== 'function') {
    if (Date.now() - started > timeoutMs) {
      throw new Error(`wasm export ${name} did not appear within ${timeoutMs}ms`)
    }
    await new Promise((resolve) => setImmediate(resolve))
  }
}

/** Drop the resident instance so a subsequent init() builds a fresh one. */
function resetInstance(): void {
  ready = false
  initPromise = null
}

/**
 * Load and start the resident wasm instance. Idempotent while it succeeds; a
 * failed init resets the singleton so callers may retry.
 */
export async function init(): Promise<void> {
  if (ready) return
  if (initPromise) return initPromise

  initPromise = (async () => {
    prepareGlobals()

    // pkg-safe: a STATIC string literal so pkg's static analysis can see the
    // asset at build time. A runtime-assembled path (path.join(...)) would be
    // invisible to pkg. (Real pkg snapshot verification is P4.)
    require('../../dist-wasm/wasm_exec.js')

    const GoCtor = (globalThis as unknown as Record<string, unknown>).Go
    if (typeof GoCtor !== 'function') {
      throw new Error('dist-wasm/wasm_exec.js did not define globalThis.Go')
    }
    if (!fs.existsSync(WASM_BIN)) {
      throw new Error(`wasm binary not found: ${WASM_BIN} (run "npm run build:wasm")`)
    }

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const go = new (GoCtor as any)()
    // Never kill the host process: an unrecoverable Go exit must be handled by
    // the loader (instance is disposable), not by process.exit.
    go.exit = (_code: number): void => {
      /* no-op */
    }
    // argv[0] must be a program-name placeholder (otherwise arg handling shifts).
    go.argv = ['uast4go.wasm']
    // Minimal env: the parser reads no environment variables, and forwarding the
    // whole process.env can overflow the fixed wasm argv/env buffer
    // ("total length of command line and environment variables exceeds limit").
    go.env = { TMPDIR: require('os').tmpdir() }

    const bytes = fs.readFileSync(WASM_BIN)
    const { instance } = await WebAssembly.instantiate(bytes, go.importObject)
    // Resident: main blocks in select{}; do NOT await run() (it never settles).
    void go.run(instance)

    await waitForExport(EXPORT_NAME)
    ready = true
  })().catch((err) => {
    // A failed init must not poison the singleton: allow a clean retry.
    resetInstance()
    throw err
  })

  return initPromise
}

export function isReady(): boolean {
  return ready
}

/**
 * Synchronously invoke the resident wasm parser. Calls are serialized by
 * construction (the Go callback is synchronous and JS cannot interleave it).
 * Never throws for `ok:false` — the Parser maps that to a JS Error and keeps
 * `errors` visible. A throw from the export (e.g. a wasm trap) drops the
 * instance so a later init() can rebuild it.
 */
export function call(request: unknown): GoResponse {
  if (!ready) {
    throw new Error('Parser is not initialized; await parser.init() first')
  }

  let raw: string
  try {
    const payload = Object.assign({ v: PROTOCOL_VERSION }, request as Record<string, unknown>)
    raw = (globalThis as unknown as Record<string, (s: string) => string>)[EXPORT_NAME](
      JSON.stringify(payload)
    )
  } catch (e) {
    resetInstance()
    throw e
  }

  let resp: GoResponse
  try {
    resp = JSON.parse(raw) as GoResponse
  } catch (e) {
    resetInstance()
    throw new Error(`invalid response from wasm: ${String(e)}`)
  }

  if (resp.v !== PROTOCOL_VERSION) {
    resetInstance()
    throw new Error(`protocol version mismatch: expected ${PROTOCOL_VERSION}, got ${String(resp.v)}`)
  }
  return resp
}
