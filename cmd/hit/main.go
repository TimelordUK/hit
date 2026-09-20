// Command hit is a multi-line-aware shell history and directory finder.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/shell"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `usage: hit <command>

commands:
  init pwsh   print the PowerShell integration; in $PROFILE:
                Invoke-Expression (& hit init pwsh | Out-String)
  path <data|history|config>
              print a resolved path (honours HIT_DATA_DIR / HIT_CONFIG)
  search      open the finder (used by the Ctrl+R handler)
  version     print the version`

func main() {
	home, _ := os.UserHomeDir()
	env := paths.Env{Getenv: os.Getenv, GOOS: runtime.GOOS, Home: home}
	os.Exit(run(os.Args[1:], env, os.Stdout, os.Stderr))
}

func run(args []string, env paths.Env, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-V":
		fmt.Fprintln(stdout, "hit", version)
		return 0
	case "init":
		return runInit(args[1:], env, stdout, stderr)
	case "search":
		return runSearch(args[1:], env, stdout, stderr)
	case "path":
		return runPath(args[1:], env, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "hit: unknown command %q\n", args[0])
	return 2
}

func runInit(args []string, env paths.Env, stdout, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "pwsh" && args[0] != "powershell") {
		fmt.Fprintln(stderr, "usage: hit init pwsh")
		return 2
	}
	hist, err := paths.History(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "hit"
	}
	if err := shell.WritePwsh(stdout, hist, exe, version); err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	return 0
}

func runPath(args []string, env paths.Env, stdout, stderr io.Writer) int {
	resolve := map[string]func(paths.Env) (string, error){
		"data":    paths.DataDir,
		"history": paths.History,
		"config":  paths.ConfigFile,
	}
	var f func(paths.Env) (string, error)
	if len(args) == 1 {
		f = resolve[args[0]]
	}
	if f == nil {
		fmt.Fprintln(stderr, "usage: hit path <data|history|config>")
		return 2
	}
	p, err := f(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	fmt.Fprintln(stdout, p)
	return 0
}
