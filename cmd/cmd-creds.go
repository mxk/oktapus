package cmd

import (
	"bufio"
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

func init() {
	register(&cmdInfo{
		names:   []string{"creds"},
		summary: "Get account credentials",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
		new:     func() Cmd { return &creds{Name: "creds"} },
	})
}

type creds struct {
	Name
	PrintFmt
	Renew  bool
	User   string
	Policy string
	Tmp    bool
	Spec   string
}

func (cmd *creds) Help(w *bufio.Writer) {
	writeHelp(w, `
		Get account credentials.

		By default, this command returns temporary credentials for all accounts
		that match the spec. These credentials are cached and are renewed only
		after expiration. You can force renewal with the -renew option.

		If you need long-term credentials, the command allows you to create an
		IAM user with an access key. If you use the -tmp option, the user will
		be automatically deleted when the account is freed.
	`)
	accountSpecHelp(w)
}

func (cmd *creds) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Renew, "renew", false,
		"Renew temporary credentials")
	fs.StringVar(&cmd.User, "user", "",
		"Get long-term credentials for the `name`d IAM user")
	fs.StringVar(&cmd.Policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set user policy `ARN`")
	fs.BoolVar(&cmd.Tmp, "tmp", false,
		"Delete this user automatically when the account is freed")
}

func (cmd *creds) Run(ctx *Ctx, args []string) error {
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *creds) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	out := listCreds(acs, cmd.Renew)
	if cmd.User == "" {
		return out, nil
	}
	user := newPathName(cmd.User)
	if cmd.Tmp {
		user.path = tmpIAMPath + user.path[1:]
	}
	creds := make(map[string]*credsOutput, len(out))
	for _, c := range out {
		creds[c.AccountID] = c
	}
	km := newKeyMaker(user.path, user.name, cmd.Policy)
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
	return out, nil
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
