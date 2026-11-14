// Command genfacade generates resources.gen.go from the SDK contract (ADR 0055, D2/D3).
//
// Usage: go run ./internal/cmd/genfacade -in api/openapi.sdk.json -out resources.gen.go
//
// The types (request/response) come from oapi-codegen (types.gen.go); this generator
// only wires each operation to a `client.<Tag>.<Action>` method.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	in := flag.String("in", "api/openapi.sdk.json", "SDK contract (OpenAPI JSON)")
	out := flag.String("out", "resources.gen.go", "generated file")
	flag.Parse()
	n, err := run(*in, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genfacade: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("resources.gen.go: %d operations\n", n)
}

func run(in, out string) (int, error) {
	raw, err := os.ReadFile(in)
	if err != nil {
		return 0, err
	}
	doc, err := parseDoc(raw)
	if err != nil {
		return 0, err
	}
	ops, err := collectOps(doc)
	if err != nil {
		return 0, err
	}
	src, err := generate(doc, ops)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return 0, err
	}
	return len(ops), nil
}
