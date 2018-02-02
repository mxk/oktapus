package main

import (
	"os"

	"github.com/LuminalHQ/oktapus/op"
)

func main() {
	op.Run(os.Args[1:])
}
