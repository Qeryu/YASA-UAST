//go:build !(js && wasm)

// Package main 在非 js/wasm 平台上是空 stub，使 `go vet ./...` 与
// `go test ./...` 在 linux/macOS 上保持全绿。真正的 wasm 入口位于
// main_js_wasm.go，由 `js && wasm` build tag 保护。
package main

func main() {}
