package cmd

import (
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	register(&Authz{command: command{
		name:    []string{"authz"},
		summary: "Authorize account access",
		usage:   "[options] account-spec role-name [role-name ...]",
		minArgs: 2,
	}})
}

type Authz struct {
	command
	desc      string
	policy    string
	principal string
	tmp       bool
}

func (cmd *Authz) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.StringVar(&cmd.desc, "desc", "",
		"Set account description")
	fs.StringVar(&cmd.policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set role policy `ARN`")
	fs.StringVar(&cmd.principal, "principal", "",
		"Override default principal `ARN` for AssumeRole policy")
	fs.BoolVar(&cmd.tmp, "tmp", false,
		"Delete this role automatically when the account is freed")
}

func (cmd *Authz) Run(ctx *Ctx, args []string) error {
	acs, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	roles := newPathNames(args[1:])
	if cmd.tmp {
		for i := range roles {
			roles[i].path = tmpIAMPath + roles[i].path[1:]
		}
	}
	if !cmd.HaveFlag(&cmd.principal) {
		cmd.principal = ctx.AWS().Ident().AccountID
	}
	assumeRolePolicy := aws.String(newAssumeRolePolicy(cmd.principal))
	var desc *string
	if cmd.HaveFlag(&cmd.desc) {
		desc = aws.String(cmd.desc)
	}
	acs.Apply(func(ac *Account) {
		if ac.Err != nil {
			return
		}
		for _, r := range roles {
			in := iam.CreateRoleInput{
				AssumeRolePolicyDocument: assumeRolePolicy,
				Description:              desc,
				Path:                     aws.String(r.path),
				RoleName:                 aws.String(r.name),
			}
			if _, ac.Err = ac.IAM.CreateRole(&in); ac.Err != nil {
				break
			}
			if cmd.policy != "" {
				in := iam.AttachRolePolicyInput{
					PolicyArn: aws.String(cmd.policy),
					RoleName:  aws.String(r.name),
				}
				if _, ac.Err = ac.IAM.AttachRolePolicy(&in); ac.Err != nil {
					break
				}
			}
		}
	})
	return cmd.PrintOutput(listResults(acs))
}
