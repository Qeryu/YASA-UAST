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
//	0 success (output written). For project mode this includes the case where
//	  some files failed to parse: the good files are emitted and the failures
//	  are reported on stderr (D11: record and continue).
//	1 single-file parse failure: the offending file is named on stderr and no
//	  output file is written.
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

func runSingleFile(file, output string) int {
	jsonBytes, errs, err := api.ParseSingleFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
		return 2
	}
	if len(errs) > 0 {
		printErrors(errs)
		return 1
	}
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
		fmt.Fprintf(os.Stderr, "%s: %s\n", e.File, e.Message)
	}
}
