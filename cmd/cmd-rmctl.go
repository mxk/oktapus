package cmd

import (
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	register(&RmCtl{command: command{
		name:    []string{"rmctl"},
		summary: "Remove account control information",
		usage:   "-confirm [account-spec]",
		maxArgs: 1,
		hidden:  true,
	}})
}

type RmCtl struct {
	command
	confirm bool
}

func (cmd *RmCtl) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.confirm, "confirm", false, "Confirm operation")
}

func (cmd *RmCtl) Run(ctx *Ctx, args []string) error {
	if !cmd.confirm {
		usageErr(cmd, "-confirm option required")
	}
	cmd.PadArgs(&args)
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
	return cmd.PrintOutput(listResults(acs))
}
