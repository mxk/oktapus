package internal

import (
	"flag"

	"github.com/golang/glog"
)

// TODO: Implement proper logging

func init() {
	flag.CommandLine.Parse([]string{"-logtostderr"}) // Configure glog
}

type fmtFunc func(format string, args ...interface{})

var Log = struct {
	I, W, E, F fmtFunc
}{
	I: glog.Infof,
	W: glog.Warningf,
	E: glog.Errorf,
	F: glog.Exitf,
}
