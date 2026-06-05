package main

import (
	"os"

	"github.com/fmilioni/envault/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	cli.Version = version
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
