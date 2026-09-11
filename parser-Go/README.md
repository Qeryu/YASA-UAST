# uast4go - UAST Go Parser

uast4go 是 YASA 项目的 Go 语言解析器，用于提取代码的统一抽象语法树（UAST）

## npm 包：`@ant-yasa/uast-parser-go`

同一份 Go parser 也以常驻 wasm 形式随 npm 分发（无需安装 Go，进程内同步调用）：

```ts
const { Parser, LanguageType, version } = require('@ant-yasa/uast-parser-go')

const parser = new Parser()
await parser.init()

const obj = parser.parseSource('examples/x.go', code)      // 同步，返回 UAST 对象
const raw = parser.parseSourceRaw('examples/x.go', code)   // 同步，返回与 CLI -single 逐字节一致的 JSON 字符串
const proj = parser.parseProject([{ name: '/abs/a.go', content: '...' }]) // 项目模式（内存）
const proj2 = parser.parseProject(files, { root: '/abs' }) // 显式 root，等价 CLI -rootDir=/abs

parser.parse(code, { sourcefile: 'x.go' })                 // java/php 风格别名
parser.lastErrors                                          // 最近一次调用的 file-level errors/warnings
```

- wasm 资产：`npm run build:wasm` → `dist-wasm/{uast4go.wasm,wasm_exec.js}`；脚本只依赖 PATH 上的 `go`（不依赖开发用 `env.sh`）。
- 测试：`npm test`（先自动 `build` + `build:wasm`，再 `node --test`）。
- 错误语义：单文件 `error` 级 → 抛错且无产物；仅 `warning` → 返回对象并在 `lastErrors` 暴露；项目模式局部失败 → 返回对象 + `lastErrors`（详见 `api` 包 D11 说明）。

## 构建二进制（支持多平台）

你可以使用 Go 原生命令构建适用于不同操作系统的可执行文件。

### 支持的平台

| OS      | Arch    | 输出文件名               |
|---------|---------|--------------------------|
| Linux   | amd64   | uast4go-linux-amd64     |
| macOS   | amd64   | uast4go-mac-amd64       |
| macOS   | arm64   | uast4go-mac-arm64       |

所有二进制均为静态链接，无需依赖外部库。

---

### 1. 构建 Linux 二进制（amd64）

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/uast4go-linux-amd64 .

---

### 2. 构建 macOS 二进制（Intel）

go build -o dist/uast4go-mac-amd64 .

---

### 3. 构建 macOS 二进制（Apple Silicon / M1/M2）

go build -o dist/uast4go-mac-arm64 .

---

### 输出目录

构建后的二进制文件将生成在：

parser-Go/dist/

请确保该目录存在，或提前创建：

mkdir -p dist

---

### CI/CD 集成

GitHub Actions 流水线会自动构建并发布以下文件：
- uast4go-linux-amd64
- uast4go-mac-amd64
- uast4go-mac-arm64

详细流程见：.github/workflows/release.yml
