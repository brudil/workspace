package main

import (
	"errors"
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
		var exit *cli.ExitCodeError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		os.Exit(1)
	}
}
