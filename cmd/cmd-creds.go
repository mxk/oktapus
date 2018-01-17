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
	renew  bool
	user   string
	policy string
	tmp    bool
}

func (cmd *Creds) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.renew, "renew", false,
		"Renew temporary credentials")
	fs.StringVar(&cmd.user, "user", "",
		"Get long-term credentials for the `name`d IAM user")
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
	out := listCreds(acs, cmd.renew)
	if cmd.user == "" {
		return cmd.PrintOutput(out)
	}
	user := newPathName(cmd.user)
	if cmd.tmp {
		user.path = tmpIAMPath + user.path[1:]
	}
	creds := make(map[string]*credsOutput, len(out))
	for _, c := range out {
		creds[c.AccountID] = c
	}
	km := newKeyMaker(user.path, user.name, cmd.policy)
	acs.Apply(func(ac *Account) {
		if ac.Err != nil {
			return
		}
		c := creds[ac.ID]
		*c = credsOutput{
			AccountID: c.AccountID,
			Name:      c.Name,
		}
		if out, err := km.newKey(ac.IAM); err == nil {
			c.AccessKeyID = aws.StringValue(out.AccessKey.AccessKeyId)
			c.SecretAccessKey = aws.StringValue(out.AccessKey.SecretAccessKey)
		} else {
			c.Error = explainError(err)
		}
	})
	return cmd.PrintOutput(out)
}

// keyMaker creates new IAM user access keys.
type keyMaker struct {
	user iam.CreateUserInput
	pol  iam.AttachUserPolicyInput
	key  iam.CreateAccessKeyInput
}

func newKeyMaker(path, user, policy string) *keyMaker {
	if path == "" {
		path = "/"
	}
	return &keyMaker{
		iam.CreateUserInput{
			Path:     aws.String(path),
			UserName: aws.String(user),
		},
		iam.AttachUserPolicyInput{
			PolicyArn: aws.String(policy),
			UserName:  aws.String(user),
		},
		iam.CreateAccessKeyInput{
			UserName: aws.String(user),
		},
	}
}

func (m *keyMaker) newKey(c *iam.IAM) (*iam.CreateAccessKeyOutput, error) {
	if _, err := c.CreateUser(&m.user); err != nil &&
		!awsErrCode(err, iam.ErrCodeEntityAlreadyExistsException) {
		return nil, err
	}
	if _, err := c.AttachUserPolicy(&m.pol); err != nil {
		return nil, err
	}
	return c.CreateAccessKey(&m.key)
}
