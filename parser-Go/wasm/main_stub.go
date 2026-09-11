//go:build !(js && wasm)

// Package main is an empty stub on non-js/wasm platforms so that
// `go vet ./...` and `go test ./...` stay green on linux/macOS. The real wasm
// entry point lives in main_js_wasm.go behind the `js && wasm` build tag.
package main

func main() {}
