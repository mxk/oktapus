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
		new:     func() Cmd { return &authz{Name: "authz"} },
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

func (cmd *authz) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	StringPtrVar(fs, &cmd.Desc, "desc",
		"Set account description")
	fs.StringVar(&cmd.Policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set role policy `ARN`")
	fs.StringVar(&cmd.Principal, "principal", "",
		"Override default principal `ARN` for AssumeRole policy")
	fs.BoolVar(&cmd.Tmp, "tmp", false,
		"Delete this role automatically when the account is freed")
}

func (cmd *authz) Run(ctx *Ctx, args []string) error {
	cmd.Spec = args[0]
	cmd.Roles = args[1:]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out.([]*resultsOutput))
	}
	return err
}

func (cmd *authz) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	roles := newPathNames(cmd.Roles)
	if cmd.Tmp {
		for i := range roles {
			roles[i].path = tmpIAMPath + roles[i].path[1:]
		}
	}
	if cmd.Principal == "" {
		cmd.Principal = ctx.AWS().Ident().AccountID
	}
	assumeRolePolicy := aws.String(newAssumeRolePolicy(cmd.Principal))
	acs.Apply(func(ac *Account) {
		if ac.Err != nil {
			return
		}
		for _, r := range roles {
			in := iam.CreateRoleInput{
				AssumeRolePolicyDocument: assumeRolePolicy,
				Description:              cmd.Desc,
				Path:                     aws.String(r.path),
				RoleName:                 aws.String(r.name),
			}
			if _, ac.Err = ac.IAM.CreateRole(&in); ac.Err != nil {
				break
			}
			if cmd.Policy != "" {
				in := iam.AttachRolePolicyInput{
					PolicyArn: aws.String(cmd.Policy),
					RoleName:  aws.String(r.name),
				}
				if _, ac.Err = ac.IAM.AttachRolePolicy(&in); ac.Err != nil {
					break
				}
			}
		}
	})
	return listResults(acs), nil
}
