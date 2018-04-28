package main

import (
	"os"

	_ "github.com/LuminalHQ/oktapus/cmd"
	"github.com/LuminalHQ/oktapus/op"
)

func main() {
	op.Run(os.Args[1:])
}
