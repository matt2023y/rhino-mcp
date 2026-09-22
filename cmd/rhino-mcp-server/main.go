package main

import (
	"fmt"
	"os"
	"rhino-mcp/internal/rhino"
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "help") {
			fmt.Println("rhino-mcp-server[.exe]: serve MCP over stdio; use rhino-tool[.exe] for config/replay")
			return
		}
		fmt.Fprintln(os.Stderr, "MCP server takes no arguments")
		os.Exit(2)
	}
	if err := rhino.RunServer(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
