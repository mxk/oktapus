package cmd

import (
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	register(&cmdInfo{
		names:   []string{"rmctl"},
		summary: "Remove account control information",
		usage:   "-confirm [account-spec]",
		maxArgs: 1,
		hidden:  true,
		new:     func() Cmd { return &rmCtl{Name: "rmctl"} },
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

func (cmd *rmCtl) Run(ctx *Ctx, args []string) error {
	if !cmd.Confirm {
		usageErr(cmd, "-confirm option required")
	}
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *rmCtl) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	acs.Apply(func(ac *Account) {
		if ac.Err == nil {
			in := iam.DeleteRoleInput{RoleName: aws.String(ctlRole)}
			_, ac.Err = ac.IAM.DeleteRole(&in)
		}
	})
	return listResults(acs), nil
}
