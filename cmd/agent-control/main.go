package main

import (
	"fmt"
	"os"

	"agent-control/internal/cli"
)

func main() {
	if err := cli.NewCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
