// Command hit is a multi-line-aware shell history and directory finder.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: hit <command>\n\ncommands:\n  version")
		return 2
	}
	switch args[0] {
	case "version", "--version", "-V":
		fmt.Fprintln(stdout, "hit", version)
		return 0
	}
	fmt.Fprintf(stderr, "hit: unknown command %q\n", args[0])
	return 2
}
