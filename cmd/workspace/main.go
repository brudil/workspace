package main

import (
	"os"
	"runtime/debug"

	"github.com/brudil/workspace/internal/cli"
)

var version = "dev"

func main() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			version = info.Main.Version
		}
	}
	cmd := cli.NewRootCmd(version)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
