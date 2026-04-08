package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	providercatalog "github.com/tiroq/arcanum/internal/agent/provider_catalog"
)

const (
	exitSuccess    = 0
	exitValidation = 1
	exitRuntime    = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	dir := flag.String("dir", "providers", "path to provider catalog directory")
	format := flag.String("format", "text", "output format: text or json")
	failOnWarning := flag.Bool("fail-on-warning", false, "treat warnings as failures")
	flag.Parse()

	if *format != "text" && *format != "json" {
		fmt.Fprintf(os.Stderr, "error: --format must be 'text' or 'json', got %q\n", *format)
		return exitRuntime
	}

	result, err := providercatalog.ValidateCatalogDir(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitRuntime
	}

	switch *format {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: marshal result: %v\n", err)
			return exitRuntime
		}
		fmt.Println(string(data))
	default:
		fmt.Print(result.Text())
	}

	if !result.Valid {
		return exitValidation
	}
	if *failOnWarning && result.WarningCount > 0 {
		return exitValidation
	}

	return exitSuccess
}
