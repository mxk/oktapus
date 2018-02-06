package cmd

import (
	"bufio"
	"flag"

	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

// TODO: List role ARNs in output. How to use -tmp for oktapus users?

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"authz"},
		Summary: "Authorize account access",
		Usage:   "[options] account-spec role-name [role-name ...]",
		MinArgs: 2,
		New:     func() op.Cmd { return &authz{Name: "authz"} },
	})
}

type authz struct {
	Name
	PrintFmt
	Desc      *string
	Policy    string
	Principal string
	Tmp       bool
	Spec      string
	Roles     []string
}

func (cmd *authz) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Authorize account access by creating a new IAM role.

		By default, this command grants other users admin access to accounts
		where you yourself have administrative privileges. For example:

			oktapus authz owner=me user1@example.com

		This command allows user1 to access all accounts that are currently
		owned by you (assuming that both of you use the same gateway account).

		You can use -principal to specify another account ID that should be
		allowed to assume the new role.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *authz) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	op.StringPtrVar(fs, &cmd.Desc, "desc",
		"Set account description")
	fs.StringVar(&cmd.Policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set role policy `ARN`")
	fs.StringVar(&cmd.Principal, "principal", "",
		"Override default principal `ARN` for AssumeRole policy")
	fs.BoolVar(&cmd.Tmp, "tmp", false,
		"Delete this role automatically when the account is freed")
}

func (cmd *authz) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec = args[0]
	cmd.Roles = args[1:]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *authz) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	if cmd.Principal == "" {
		cmd.Principal = ctx.AWS().Ident().AccountID
	}
	assumeRolePolicy := op.NewAssumeRolePolicy(cmd.Principal).Doc()
	roles := make([]*iam.CreateRoleInput, len(cmd.Roles))
	for i, r := range cmd.Roles {
		path, name := op.SplitPath(r)
		if cmd.Tmp {
			path = op.TmpIAMPath + path[1:]
		}
		roles[i] = &iam.CreateRoleInput{
			AssumeRolePolicyDocument: assumeRolePolicy,
			Description:              cmd.Desc,
			Path:                     aws.String(path),
			RoleName:                 aws.String(name),
		}
	}
	acs.Apply(func(ac *op.Account) {
		if ac.Err != nil {
			return
		}
		for _, r := range roles {
			if _, ac.Err = ac.IAM().CreateRole(r); ac.Err != nil {
				break
			}
			if cmd.Policy != "" {
				in := iam.AttachRolePolicyInput{
					PolicyArn: aws.String(cmd.Policy),
					RoleName:  r.RoleName,
				}
				if _, ac.Err = ac.IAM().AttachRolePolicy(&in); ac.Err != nil {
					break
				}
			}
		}
	})
	return listResults(acs), nil
}
