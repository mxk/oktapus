package cmd

import (
	"flag"

	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"rmctl"},
		Summary: "Remove account control information",
		Usage:   "-confirm [account-spec]",
		MaxArgs: 1,
		Hidden:  true,
		New:     func() op.Cmd { return &rmCtl{Name: "rmctl"} },
	})
}

type rmCtl struct {
	Name
	PrintFmt
	Confirm bool
	Spec    string
}

func (cmd *rmCtl) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Confirm, "confirm", false, "Confirm operation")
}

func (cmd *rmCtl) Run(ctx *op.Ctx, args []string) error {
	if !cmd.Confirm {
		op.UsageErr(cmd, "-confirm option required")
	}
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *rmCtl) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	acs.Apply(func(ac *op.Account) {
		if ac.Err == nil {
			in := iam.DeleteRoleInput{RoleName: aws.String(op.CtlRole)}
			_, ac.Err = ac.IAM.DeleteRole(&in)
		}
	})
	return listResults(acs), nil
}
