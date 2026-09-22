package main

import (
	"fmt"
	"os"
	"rhino-mcp/internal/rhino"
)

func main() {
	if err := rhino.RunTool(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
