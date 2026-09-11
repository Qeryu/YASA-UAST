import { call, init as initLoader, isReady, ParseError } from './loader'

export type { ParseError } from './loader'

/** Language discriminator, aligned with the java/js/php parser packages. */
export enum LanguageType {
  LANG_GO = 'go',
  OTHER = 'other',
}

/** In-memory source file for {@link Parser.parseProject}. */
export interface SourceFile {
  name: string
  content: string
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
   * Parse a set of in-memory source files as a project (mirrors the CLI
   * `-rootDir` package tree). Returns the UAST object; file-level errors are in
   * {@link lastErrors}.
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parseProject(files: SourceFile[]): any {
    this.ensureInit()
    const resp = call({
      mode: 'project',
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
