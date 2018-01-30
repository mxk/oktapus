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
		new:     func() Cmd { return &RmCtl{Name: "rmctl"} },
	})
}

type RmCtl struct {
	Name
	PrintFmt
	confirm bool
}

func (cmd *RmCtl) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.confirm, "confirm", false, "Confirm operation")
}

func (cmd *RmCtl) Run(ctx *Ctx, args []string) error {
	if !cmd.confirm {
		usageErr(cmd, "-confirm option required")
	}
	padArgs(cmd, &args)
	acs, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	acs.Apply(func(ac *Account) {
		if ac.Err == nil {
			in := iam.DeleteRoleInput{RoleName: aws.String(ctlRole)}
			_, ac.Err = ac.IAM.DeleteRole(&in)
		}
	})
	return cmd.Print(listResults(acs))
}
