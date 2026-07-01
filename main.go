package main

import (
	"os"

	"github.com/MaxAnderson95/keep/internal/cli"
)

// Build metadata, injected via -ldflags at release time (goreleaser).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Run(os.Args, cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}))
}
