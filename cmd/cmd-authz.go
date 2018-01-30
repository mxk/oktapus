package cmd

import (
	"bufio"
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

// TODO: List role ARNs in output. How to use -tmp for oktapus users?

func init() {
	register(&cmdInfo{
		names:   []string{"authz"},
		summary: "Authorize account access",
		usage:   "[options] account-spec role-name [role-name ...]",
		minArgs: 2,
		new:     func() Cmd { return &Authz{Name: "authz"} },
	})
}

type Authz struct {
	Name
	PrintFmt
	desc      *string
	policy    string
	principal string
	tmp       bool
}

func (cmd *Authz) Help(w *bufio.Writer) {
	writeHelp(w, `
		Authorize account access by creating a new IAM role.

		By default, this command grants other users admin access to accounts
		where you yourself have administrative privileges. For example:

			oktapus authz owner=me user1@example.com

		This command allows user1 to access all accounts that are currently
		owned by you (assuming that both of you use the same gateway account).

		You can use -principal to specify another account ID that should be
		allowed to assume the new role.
	`)
	accountSpecHelp(w)
}

func (cmd *Authz) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	StringPtrVar(fs, &cmd.desc, "desc",
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
	if cmd.principal == "" {
		cmd.principal = ctx.AWS().Ident().AccountID
	}
	assumeRolePolicy := aws.String(newAssumeRolePolicy(cmd.principal))
	acs.Apply(func(ac *Account) {
		if ac.Err != nil {
			return
		}
		for _, r := range roles {
			in := iam.CreateRoleInput{
				AssumeRolePolicyDocument: assumeRolePolicy,
				Description:              cmd.desc,
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
	return cmd.Print(listResults(acs))
}
