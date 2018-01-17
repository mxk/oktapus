package cmd

import (
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	register(&Creds{command: command{
		name:    []string{"creds"},
		summary: "Get account credentials",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
	}})
}

type Creds struct {
	command
	user   string
	policy string
	tmp    bool
}

func (cmd *Creds) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.StringVar(&cmd.user, "user", "",
		"Get long-term credentials for a new user with the given `name`")
	fs.StringVar(&cmd.policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set user policy `ARN`")
	fs.BoolVar(&cmd.tmp, "tmp", false,
		"Delete this user automatically when the account is freed")
}

func (cmd *Creds) Run(ctx *Ctx, args []string) error {
	acs, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	out := listCreds(acs)
	if cmd.user != "" {
		user := newPathName(cmd.user)
		if cmd.tmp {
			user.path = tmpIAMPath + user.path[1:]
		}
		creds := make(map[string]*credsOutput, len(out))
		for _, c := range out {
			creds[c.AccountID] = c
		}
		inUser := iam.CreateUserInput{
			Path:     aws.String(user.path),
			UserName: aws.String(user.name),
		}
		inPol := iam.AttachUserPolicyInput{
			PolicyArn: aws.String(cmd.policy),
			UserName:  aws.String(user.name),
		}
		inKey := iam.CreateAccessKeyInput{
			UserName: aws.String(user.name),
		}
		acs.Apply(func(ac *Account) {
			c := creds[ac.ID]
			defer func() {
				c.Error = explainError(ac.Err)
			}()
			var out *iam.CreateAccessKeyOutput
			if ac.Err != nil {
				return
			} else if _, ac.Err = ac.IAM.CreateUser(&inUser); ac.Err != nil {
				return
			} else if _, ac.Err = ac.IAM.AttachUserPolicy(&inPol); ac.Err != nil {
				return
			} else if out, ac.Err = ac.IAM.CreateAccessKey(&inKey); ac.Err != nil {
				return
			}
			c.Expires = ""
			c.AccessKeyID = aws.StringValue(out.AccessKey.AccessKeyId)
			c.SecretAccessKey = aws.StringValue(out.AccessKey.SecretAccessKey)
			c.SessionToken = ""
		})
	}
	return cmd.PrintOutput(out)
}
