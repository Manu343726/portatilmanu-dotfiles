package main

import (
	"os"
)

var buildHash string

func main() {
	root := newRootCmd()

	// Best-effort: register dynamic commands (plugin tools, registered scripts)
	// as top-level builtins. If the daemon isn't reachable on the default port,
	// only the static core commands will be available. PersistentPreRunE will
	// still attempt connection later (with full --port resolution).
	port := os.Getenv("DOTFILESD_PORT")
	if port == "" {
		port = "9105"
	}
	registerDynamicCommands(root, port)

	if err := root.Execute(); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
