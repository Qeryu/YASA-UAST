import { call, init as initLoader, isReady, ParseError } from './loader'

export type { ParseError } from './loader'

// eslint-disable-next-line @typescript-eslint/no-var-requires
const pkg = require('../../package.json') as { version?: string }

/** Package version (mirrors package.json; injected at publish time in CI). */
export const version: string = pkg.version ?? '0.0.0'

/** Language discriminator, aligned with the java/js/php parser packages. */
export enum LanguageType {
  // The Engine keys Go by the string 'golang' (parser.ts), not 'go'.
  LANG_GO = 'golang',
  OTHER = 'other',
}

/** In-memory source file for {@link Parser.parseProject}. */
export interface SourceFile {
  name: string
  content: string
}

/** Options accepted by {@link Parser.parse} (java/php-style `sourcefile`). */
export interface ParseOptions {
  sourcefile?: string
}

/** Options accepted by {@link Parser.parseProject}. */
export interface ParseProjectOptions {
  /** Explicit module root, matching the CLI `-rootDir` value. */
  root?: string
}

/**
 * In-process Go UAST parser backed by a resident wasm instance.
 *
 * Usage:
 * ```ts
 * const { Parser } = require('@ant-yasa/uast-parser-go')
 * const p = new Parser()
 * await p.init()
 * const obj = p.parseSource('examples/x.go', code)
 * const raw = p.parseSourceRaw('examples/x.go', code) // CLI-identical JSON string
 * const proj = p.parseProject([{ name: '/abs/a.go', content: '...' }])
 * ```
 */
export class Parser {
  private initialized = false
  private lastErrorList: ParseError[] = []

  /**
   * Options are accepted for symmetry with the java/js/php parser packages and
   * are currently ignored.
   */
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  public constructor(_opts?: Record<string, unknown>) {}

  /** Load the wasm instance. Must be awaited once before any parse call. */
  public async init(): Promise<void> {
    await initLoader()
    this.initialized = true
  }

  /** File-level errors from the most recent call (warnings included). */
  public get lastErrors(): ParseError[] {
    return this.lastErrorList
  }

  /**
   * Parse one in-memory source file and return the raw CLI-equivalent JSON
   * string (including the trailing newline written by the CLI Encoder).
   */
  public parseSourceRaw(name: string, content: string): string {
    this.ensureInit()
    const resp = call({ mode: 'single', name, content })
    this.lastErrorList = resp.errors ?? []
    if (!resp.ok) {
      throw this.failureError(resp.errors ?? [])
    }
    if (typeof resp.data !== 'string') {
      throw new Error('wasm response missing data')
    }
    return resp.data
  }

  /** Parse one in-memory source file and return the UAST object. */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parseSource(name: string, content: string): any {
    return JSON.parse(this.parseSourceRaw(name, content))
  }

  /**
   * java/php-style alias: parse content, using `opts.sourcefile` as the
   * loc.sourcefile name (falls back to '').
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parse(content: string, opts?: ParseOptions): any {
    return this.parseSource(opts?.sourcefile ?? '', content)
  }

  /**
   * Parse a set of in-memory source files as a project (mirrors the CLI
   * `-rootDir` package tree). Pass `opts.root` to make the virtual root
   * explicit instead of using the common-ancestor heuristic. Returns the UAST
   * object; file-level errors are in {@link lastErrors}.
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parseProject(files: SourceFile[], opts?: ParseProjectOptions): any {
    this.ensureInit()
    const resp = call({
      mode: 'project',
      root: opts?.root ?? '',
      files: files.map((f) => ({ name: f.name, content: f.content })),
    })
    this.lastErrorList = resp.errors ?? []
    if (!resp.ok) {
      throw this.failureError(resp.errors ?? [])
    }
    if (typeof resp.data !== 'string') {
      throw new Error('wasm response missing data')
    }
    return JSON.parse(resp.data)
  }

  private ensureInit(): void {
    if (!this.initialized && !isReady()) {
      throw new Error('Parser is not initialized; await parser.init() first')
    }
  }

  private failureError(errors: ParseError[]): Error {
    const detail = errors
      .map((e) => `${e.file || '<request>'}: [${e.severity}/${e.kind}] ${e.message}`)
      .join('; ')
    const err = new Error(`uast parse failed: ${detail || 'unknown error'}`)
    ;(err as Error & { errors?: ParseError[] }).errors = errors
    return err
  }
}
