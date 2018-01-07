package main

import (
	"os"

	"oktapus/cmd"
)

func main() {
	cmd.Run(os.Args[1:])
}
