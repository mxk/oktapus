package main

import (
	"os"

	"github.com/LuminalHQ/oktapus/cmd"
)

func main() {
	cmd.Run(os.Args[1:])
}
