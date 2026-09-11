import { call, init as initLoader, isReady, ParseError } from './loader'

export type { ParseError } from './loader'

// eslint-disable-next-line @typescript-eslint/no-var-requires
const pkg = require('../../package.json') as { version?: string }

/** 包版本号（来自 package.json；CI 发布时注入真实版本）。 */
export const version: string = pkg.version ?? '0.0.0'

/** 语言标识，与 java/js/php parser 包对齐。 */
export enum LanguageType {
  // Engine 侧 Go 的语言字符串是 'golang'（parser.ts），不是 'go'。
  LANG_GO = 'golang',
  OTHER = 'other',
}

/** {@link Parser.parseProject} 使用的内存源码文件。 */
export interface SourceFile {
  name: string
  content: string
}

/** {@link Parser.parse} 的选项（java/php 风格的 `sourcefile`）。 */
export interface ParseOptions {
  sourcefile?: string
}

/** {@link Parser.parseProject} 的选项。 */
export interface ParseProjectOptions {
  /** 显式 module root，等价 CLI 的 `-rootDir`。 */
  root?: string
}

/**
 * 基于常驻 wasm 实例的进程内 Go UAST parser。
 *
 * 用法：
 * ```ts
 * const { Parser } = require('@ant-yasa/uast-parser-go')
 * const p = new Parser()
 * await p.init()
 * const obj = p.parseSource('examples/x.go', code)
 * const raw = p.parseSourceRaw('examples/x.go', code) // 与 CLI 逐字节一致的 JSON 字符串
 * const proj = p.parseProject([{ name: '/abs/a.go', content: '...' }])
 * ```
 */
export class Parser {
  private initialized = false
  private lastErrorList: ParseError[] = []

  /**
   * 接受选项仅为了与 java/js/php parser 包保持接口对称，当前忽略。
   */
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  public constructor(_opts?: Record<string, unknown>) {}

  /** 加载 wasm 实例。任何 parse 调用前需 await 一次。 */
  public async init(): Promise<void> {
    await initLoader()
    this.initialized = true
  }

  /** 最近一次调用的 file-level 错误（含 warning）。 */
  public get lastErrors(): ParseError[] {
    return this.lastErrorList
  }

  /**
   * 解析单个内存源码文件，返回与 CLI 等价的原始 JSON 字符串
   * （含 CLI Encoder 写入的末尾换行）。
   */
  public parseSourceRaw(name: string, content: string): string {
    this.ensureInit()
    const resp = call({ mode: 'single', name, content })
    this.lastErrorList = resp.errors ?? []
    if (!resp.ok) {
      throw this.failureError(resp.errors ?? [])
    }
    if (typeof resp.data !== 'string') {
      throw new Error('wasm 响应缺少 data 字段')
    }
    return resp.data
  }

  /** 解析单个内存源码文件，返回 UAST 对象。 */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parseSource(name: string, content: string): any {
    return JSON.parse(this.parseSourceRaw(name, content))
  }

  /**
   * java/php 风格别名：解析 content，用 `opts.sourcefile` 作为
   * loc.sourcefile 名称（缺省为 ''）。
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public parse(content: string, opts?: ParseOptions): any {
    return this.parseSource(opts?.sourcefile ?? '', content)
  }

  /**
   * 以项目模式解析一组内存源码文件（复刻 CLI `-rootDir` 的包树）。
   * 传入 `opts.root` 可显式指定虚拟 root，替代共同祖先启发式。
   * 返回 UAST 对象；file-level 错误见 {@link lastErrors}。
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
      throw new Error('wasm 响应缺少 data 字段')
    }
    return JSON.parse(resp.data)
  }

  private ensureInit(): void {
    if (!this.initialized && !isReady()) {
      throw new Error('Parser 未初始化，请先 await parser.init()')
    }
  }

  private failureError(errors: ParseError[]): Error {
    const detail = errors
      .map((e) => `${e.file || '<request>'}: [${e.severity}/${e.kind}] ${e.message}`)
      .join('; ')
    const err = new Error(`uast 解析失败: ${detail || '未知错误'}`)
    ;(err as Error & { errors?: ParseError[] }).errors = errors
    return err
  }
}
