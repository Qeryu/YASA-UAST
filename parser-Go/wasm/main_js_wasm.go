//go:build js && wasm

// Package main 是 @ant-yasa/uast-parser-go 使用的常驻（长生命周期）wasm 入口。
// P3 使用 syscall/js；//go:wasmexport 是 P5 的运输层切换。它不经过 CLI 的
// main / flag.Parse。
//
// 协议（同步，JSON 字符串进 / JSON 字符串出）。该 envelope 为本包与 loader 的
// 内部约定；"v" 是协议版本（1），使 TS loader 能拒绝形状不匹配而不是静默误解析。
//
//	request  {"v":1,"mode":"single","name":"...","content":"..."}
//	         {"v":1,"mode":"project","root":"...","files":[{"name":"...","content":"..."}]}
//	response {"v":1,"ok":true,"data":"<与 CLI 等价的 JSON 字符串，含末尾换行>","errors":[...]}
//	         {"v":1,"ok":false,"errors":[...]}
//
// 单文件的 error 级错误返回 ok=false（无产物）；项目模式返回 ok=true 并带 data
// 和 errors（局部失败），与 CLI 一致。handler 绝不 panic/exit：api.* 已 recover，
// handle() 再加一层协议边界 recover。
package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"uast4go/api"
)

const protocolVersion = 1

type parseRequest struct {
	V       int          `json:"v"`
	Mode    string       `json:"mode"`
	Name    string       `json:"name"`
	Content string       `json:"content"`
	Root    string       `json:"root"`
	Files   []sourceFile `json:"files"`
}

type sourceFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type parseResponse struct {
	V      int              `json:"v"`
	OK     bool             `json:"ok"`
	Data   string           `json:"data,omitempty"`
	Errors []api.ParseError `json:"errors,omitempty"`
}

func main() {
	js.Global().Set("__uastGoParse", js.FuncOf(handle))

	// 常驻：保持 Go runtime（及已注册的回调）存活，同时让 JS 事件循环空出来
	// 供同步调用使用。
	select {}
}

func handle(this js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = marshal(parseResponse{
				OK:     false,
				Errors: protocolError(fmt.Sprintf("recover 捕获到 panic: %v", r)),
			})
		}
	}()

	if len(args) < 1 || args[0].Type() != js.TypeString {
		return marshal(parseResponse{OK: false, Errors: protocolError("期望一个 JSON 请求字符串")})
	}

	var req parseRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return marshal(parseResponse{OK: false, Errors: protocolError("请求 JSON 非法: " + err.Error())})
	}

	switch req.Mode {
	case "single":
		data, errs, err := api.ParseSource(req.Name, []byte(req.Content))
		if err != nil {
			return marshal(failure(errs, err))
		}
		if api.HasErrors(errs) {
			// 单文件：error 级表示无产物（CLI exit-1 语义）。
			return marshal(parseResponse{V: protocolVersion, OK: false, Errors: errs})
		}
		return marshal(parseResponse{V: protocolVersion, OK: true, Data: string(data), Errors: errs})
	case "project":
		files := make([]api.SourceFile, 0, len(req.Files))
		for _, f := range req.Files {
			files = append(files, api.SourceFile{Name: f.Name, Content: []byte(f.Content)})
		}
		data, errs, err := api.ParseSources(files, req.Root)
		if err != nil {
			return marshal(failure(errs, err))
		}
		return marshal(parseResponse{V: protocolVersion, OK: true, Data: string(data), Errors: errs})
	default:
		return marshal(parseResponse{V: protocolVersion, OK: false, Errors: protocolError("未知 mode: " + req.Mode)})
	}
}

func failure(errs []api.ParseError, err error) parseResponse {
	out := append([]api.ParseError{}, errs...)
	out = append(out, api.ParseError{
		Message:  err.Error(),
		Severity: api.SeverityError,
		Kind:     api.KindParseError,
	})
	return parseResponse{V: protocolVersion, OK: false, Errors: out}
}

func protocolError(message string) []api.ParseError {
	return []api.ParseError{{
		Message:  message,
		Severity: api.SeverityError,
		Kind:     api.KindParseError,
	}}
}

func marshal(resp parseResponse) string {
	resp.V = protocolVersion
	b, err := json.Marshal(resp)
	if err != nil {
		return `{"v":1,"ok":false,"errors":[{"message":"响应序列化失败","severity":"error","kind":"parse_error"}]}`
	}
	return string(b)
}
