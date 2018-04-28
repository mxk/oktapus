package main

import (
	"os"

	_ "github.com/LuminalHQ/cloudcover/oktapus/cmd"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

func main() {
	op.Run(os.Args[1:])
}
