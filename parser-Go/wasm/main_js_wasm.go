//go:build js && wasm

// Package main 是 @ant-yasa/uast-parser-go 使用的常驻（长生命周期）wasm 入口。
// P5 使用 //go:wasmexport 导出（此前的 syscall/js 运输层已移除）。它不经过 CLI 的
// main / flag.Parse。
//
// 协议（同步；宿主写线性内存 + JSON envelope，envelope 内容与 P3 一致）：
//
//	宿主: ptr = uast_alloc(len); 将请求字节写入 wasm 线性内存 [ptr, ptr+len)
//	宿主: packed = uast_parse(ptr, len) // int64 = (resultPtr<<32) | resultLen
//	宿主: 从线性内存 [resultPtr, resultPtr+resultLen) 读回响应 JSON
//
//	request  {"v":1,"mode":"single","name":"...","content":"..."}
//	         {"v":1,"mode":"project","root":"...","files":[{"name":"...","content":"..."}]}
//	response {"v":1,"ok":true,"data":"<与 CLI 等价的 JSON 字符串，含末尾换行>","errors":[...]}
//	         {"v":1,"ok":false,"errors":[...]}
//
// 单文件的 error 级错误返回 ok=false（无产物）；项目模式返回 ok=true 并带 data
// 和 errors（局部失败），与 CLI 一致。导出函数绝不 panic/exit：parseExport 内
// recover，api.* 也各自 recover。main 阻塞在 select{}，保持 runtime 存活，宿主方可
// 在 go.run() 之后同步调用导出。
package main

import (
	"encoding/json"
	"fmt"
	"unsafe"

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

var (
	// requestBuf 保持宿主写入的请求字节存活到 uast_parse 读取。
	requestBuf []byte
	// requestPtr 是 uast_alloc 返回、宿主写入所用的指针。
	requestPtr int32
	// resultBuf 保持 uast_parse 结果存活到宿主读回。
	resultBuf []byte
)

// uast_alloc 在 Go 堆上分配 size 字节并返回其在线性内存中的指针（0 表示空）。
// 宿主随后把请求字节写入该区间，再调用 uast_parse。
//
//go:wasmexport uast_alloc
func uastAlloc(size int32) int32 {
	if size <= 0 {
		requestBuf, requestPtr = nil, 0
		return 0
	}
	requestBuf = make([]byte, int(size))
	requestPtr = int32(uintptr(unsafe.Pointer(&requestBuf[0])))
	return requestPtr
}

// uast_parse 解析宿主写入的请求并返回打包结果 (resultPtr<<32)|resultLen。
// 参数 ptr 必须与 uast_alloc 返回的指针一致。
//
//go:wasmexport uast_parse
func uastParse(ptr int32, length int32) int64 {
	return parseExport(ptr, length)
}

func parseExport(ptr, length int32) (packed int64) {
	defer func() {
		if r := recover(); r != nil {
			packed = storeResult(parseResponse{
				OK:     false,
				Errors: protocolError(fmt.Sprintf("recover 捕获到 panic: %v", r)),
			})
		}
	}()

	if length < 0 || int(length) > len(requestBuf) || (length > 0 && ptr != requestPtr) {
		return storeResult(parseResponse{OK: false, Errors: protocolError("请求缓冲区不匹配")})
	}

	var req parseRequest
	if err := json.Unmarshal(requestBuf[:length], &req); err != nil {
		return storeResult(parseResponse{OK: false, Errors: protocolError("请求 JSON 非法: " + err.Error())})
	}
	return storeResult(dispatch(req))
}

func dispatch(req parseRequest) parseResponse {
	switch req.Mode {
	case "single":
		data, errs, err := api.ParseSource(req.Name, []byte(req.Content))
		if err != nil {
			return failure(errs, err)
		}
		if api.HasErrors(errs) {
			// 单文件：error 级表示无产物（CLI exit-1 语义）。
			return parseResponse{OK: false, Errors: errs}
		}
		return parseResponse{OK: true, Data: string(data), Errors: errs}
	case "project":
		files := make([]api.SourceFile, 0, len(req.Files))
		for _, f := range req.Files {
			files = append(files, api.SourceFile{Name: f.Name, Content: []byte(f.Content)})
		}
		data, errs, err := api.ParseSources(files, req.Root)
		if err != nil {
			return failure(errs, err)
		}
		return parseResponse{OK: true, Data: string(data), Errors: errs}
	default:
		return parseResponse{OK: false, Errors: protocolError("未知 mode: " + req.Mode)}
	}
}

// storeResult 序列化响应、保活结果缓冲并打包返回 (ptr<<32)|len。
func storeResult(resp parseResponse) int64 {
	resp.V = protocolVersion
	raw, err := json.Marshal(resp)
	if err != nil {
		raw = []byte(`{"v":1,"ok":false,"errors":[{"message":"响应序列化失败","severity":"error","kind":"parse_error"}]}`)
	}
	resultBuf = raw
	if len(raw) == 0 {
		return 0
	}
	p := uint64(uintptr(unsafe.Pointer(&resultBuf[0])))
	return int64(p<<32 | uint64(uint32(len(raw))))
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

func main() {
	// 常驻：保持 runtime 存活，使宿主可在 go.run() 后同步调用导出。
	select {}
}
