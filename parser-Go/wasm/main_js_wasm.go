//go:build js && wasm

// Package main is the P3 resident (long-lived) wasm entry point skeleton.
//
// P2-R only establishes the build-tagged entry point (B2): it registers a
// placeholder export and blocks forever, so the Go runtime and any registered
// callbacks stay alive across calls (D7/D8). It deliberately does NOT go
// through the CLI main / flag.Parse.
//
// P3 scope (not specified here): the real transport (syscall/js FuncOf vs
// //go:wasmexport), the request/response protocol, and error marshalling.
// The in-memory core it will call is api.parseSource — see api/contract_test.go
// for the native byte-identity contract it must preserve.
package main

import "syscall/js"

func main() {
	// Placeholder export; P3 replaces this with the parse entry point.
	js.Global().Set("__uast_go_skeleton", js.FuncOf(func(this js.Value, args []js.Value) any {
		return "uast4go wasm skeleton"
	}))

	// Resident: keep the runtime alive. The event loop stays free for future
	// synchronous js.FuncOf callbacks.
	select {}
}
