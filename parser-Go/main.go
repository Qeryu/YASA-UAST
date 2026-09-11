package main

import (
	"flag"
	"fmt"
	"os"

	"uast4go/api"
)

// CLI contract (frozen): -rootDir / -single / -output.
//
// Exit codes:
//
//	0 success (output written). This includes:
//	  - project mode with file-level errors: good files are emitted and the
//	    failures are reported on stderr (D11: record and continue);
//	  - single-file mode with warning-severity entries only (e.g. an unsupported
//	    node degraded to Noop): the product is still written.
//	1 single-file parse/read failure (error-severity entry): the offending file
//	  is named on stderr and no output file is written.
//	2 request-level failure (missing rootDir, no .go files, write error, or a
//	  residual recovered panic): nothing is written unless otherwise noted.
func main() {
	var rootDir string
	var singleFileParse bool
	var output string
	flag.StringVar(&rootDir, "rootDir", "", "The root directory of the Go project")
	flag.BoolVar(&singleFileParse, "single", false, "is single file parse")
	flag.StringVar(&output, "output", "", "The output path of Go UAST")
	flag.Parse()

	if singleFileParse {
		os.Exit(runSingleFile(rootDir, output))
	}
	os.Exit(runProject(rootDir, output))
}

// parseSingleFunc is a seam so the exit-code semantics can be unit-tested
// without a synthetic Go source that triggers builder-level degradation.
type parseSingleFunc func(path string) ([]byte, []api.ParseError, error)

func runSingleFile(file, output string) int {
	return runSingleFileWith(api.ParseSingleFile, file, output)
}

func runSingleFileWith(parse parseSingleFunc, file, output string) int {
	jsonBytes, errs, err := parse(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
		return 2
	}
	printErrors(errs)
	if api.HasErrors(errs) {
		// Parse/read failure: no product.
		return 1
	}
	if jsonBytes == nil {
		fmt.Fprintf(os.Stderr, "%s: no output produced\n", file)
		return 1
	}
	// Warnings only: still emit the product (consistent with project mode).
	if err := os.WriteFile(output, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "无法创建文件: %v\n", err)
		return 2
	}
	return 0
}

func runProject(rootDir, output string) int {
	jsonBytes, errs, err := api.ParseProject(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", rootDir, err)
		return 2
	}
	// File-level failures: report them, but the batch still succeeds.
	printErrors(errs)
	if err := os.WriteFile(output, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "无法创建文件: %v\n", err)
		return 2
	}
	return 0
}

func printErrors(errs []api.ParseError) {
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "%s: [%s/%s] %s\n", e.File, e.Severity, e.Kind, e.Message)
	}
}
