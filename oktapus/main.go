package main

import (
	"os"

	"github.com/mxk/go-cli"

	// CLI registration
	_ "github.com/mxk/oktapus/cmd"
)

func main() {
	cli.DebugFromEnv("OKTAPUS_DEBUG")
	cli.Main.Summary = "AWS account management and creation tool"
	cli.Main.Run(os.Args[1:])
}
