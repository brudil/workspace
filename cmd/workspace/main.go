package main

import (
	"os"

	"github.com/brudil/workspace/internal/cli"
)

var version = "dev"

func main() {
	cmd := cli.NewRootCmd(version)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
