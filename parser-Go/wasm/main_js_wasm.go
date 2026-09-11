//go:build js && wasm

// Package main implements the resident (long-lived) wasm entry point used by
// @ant-yasa/uast-parser-go. P3 uses syscall/js; //go:wasmexport is the P5
// transport swap. It does NOT go through the CLI main / flag.Parse.
//
// Protocol (synchronous, JSON string in / JSON string out):
//
//	request  {"mode":"single","name":"...","content":"..."}
//	         {"mode":"project","files":[{"name":"...","content":"..."}]}
//	response {"ok":true,"data":"<CLI JSON string, incl. trailing newline>","errors":[...]}
//	         {"ok":false,"errors":[...]}
//
// Single-file error-severity errors yield ok=false (no product); project mode
// yields ok=true with data plus errors (partial failure), matching the CLI.
// The handler never panics/exits: api.* already recovers and handle() adds a
// protocol-boundary recover.
package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"uast4go/api"
)

type parseRequest struct {
	Mode    string       `json:"mode"`
	Name    string       `json:"name"`
	Content string       `json:"content"`
	Files   []sourceFile `json:"files"`
}

type sourceFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type parseResponse struct {
	OK     bool             `json:"ok"`
	Data   string           `json:"data,omitempty"`
	Errors []api.ParseError `json:"errors,omitempty"`
}

func main() {
	js.Global().Set("__uastGoParse", js.FuncOf(handle))

	// Resident: keep the Go runtime (and the registered callback) alive while
	// leaving the JS event loop free for synchronous calls.
	select {}
}

func handle(this js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = marshal(parseResponse{
				OK:     false,
				Errors: protocolError(fmt.Sprintf("recovered panic: %v", r)),
			})
		}
	}()

	if len(args) < 1 || args[0].Type() != js.TypeString {
		return marshal(parseResponse{OK: false, Errors: protocolError("expected a JSON request string")})
	}

	var req parseRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return marshal(parseResponse{OK: false, Errors: protocolError("invalid request JSON: " + err.Error())})
	}

	switch req.Mode {
	case "single":
		data, errs, err := api.ParseSource(req.Name, []byte(req.Content))
		if err != nil {
			return marshal(failure(errs, err))
		}
		if api.HasErrors(errs) {
			// Single-file: error-severity means no product (CLI exit-1 semantics).
			return marshal(parseResponse{OK: false, Errors: errs})
		}
		return marshal(parseResponse{OK: true, Data: string(data), Errors: errs})
	case "project":
		files := make([]api.SourceFile, 0, len(req.Files))
		for _, f := range req.Files {
			files = append(files, api.SourceFile{Name: f.Name, Content: []byte(f.Content)})
		}
		data, errs, err := api.ParseSources(files)
		if err != nil {
			return marshal(failure(errs, err))
		}
		return marshal(parseResponse{OK: true, Data: string(data), Errors: errs})
	default:
		return marshal(parseResponse{OK: false, Errors: protocolError("unknown mode: " + req.Mode)})
	}
}

func failure(errs []api.ParseError, err error) parseResponse {
	out := append([]api.ParseError{}, errs...)
	out = append(out, api.ParseError{
		Message:  err.Error(),
		Severity: api.SeverityError,
		Kind:     api.KindParseError,
	})
	return parseResponse{OK: false, Errors: out}
}

func protocolError(message string) []api.ParseError {
	return []api.ParseError{{
		Message:  message,
		Severity: api.SeverityError,
		Kind:     api.KindParseError,
	}}
}

func marshal(resp parseResponse) string {
	b, err := json.Marshal(resp)
	if err != nil {
		return `{"ok":false,"errors":[{"message":"failed to marshal response","severity":"error","kind":"parse_error"}]}`
	}
	return string(b)
}
