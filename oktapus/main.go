package main

import (
	"os"

	"github.com/LuminalHQ/cloudcover/x/cli"

	// CLI registration
	_ "github.com/LuminalHQ/cloudcover/oktapus/cmd"
)

func main() {
	cli.DebugFromEnv("OKTAPUS_DEBUG")
	cli.Main.Summary = "AWS account management and creation tool"
	cli.Main.Run(os.Args[1:])
}
