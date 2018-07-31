package cmd

import (
	"errors"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var credsCli = register(&cli.Info{
	Name:    "creds",
	Usage:   "[options] account-spec",
	Summary: "Get account credentials",
	MinArgs: 1,
	MaxArgs: 1,
	New:     func() cli.Cmd { return &credsCmd{} },
})

type credsCmd struct {
	OutFmt
	Renew  bool   `flag:"Renew temporary credentials"`
	User   string `flag:"Get long-term credentials for the <name>d IAM user"`
	Policy string `flag:"Set user policy <ARN> (default is AdministratorAccess)"`
	Tmp    bool   `flag:"Delete this user automatically when the account is freed"`
	Spec   string
}

func (cmd *credsCmd) Info() *cli.Info { return credsCli }

func (cmd *credsCmd) Help(w *cli.Writer) {
	w.Text(`
	Get account credentials.

	By default, this command returns temporary credentials for all accounts that
	match the spec. These credentials are cached and are renewed only after
	expiration. You can force renewal with the -renew option.

	If you need long-term credentials, the command allows you to create an IAM
	user with an access key. If you use the -tmp option, the user will be
	automatically deleted when the account is freed.
	`)
	accountSpecHelp(w)
}

func (cmd *credsCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *credsCmd) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *credsCmd) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	out := listCreds(acs, cmd.Renew)
	if cmd.User == "" {
		return out, nil
	}
	user := (arn.Base + "user/").WithPathName(cmd.User)
	if cmd.Tmp {
		user = user.WithPath(op.TmpIAMPath + user.Path())
	}
	policy := arn.ARN(cmd.Policy)
	if policy == "" {
		part := ctx.Gateway().Ident().Partition()
		policy = adminAccess.WithPartition(part)
	}
	km := newKeyMaker(user, policy)
	acs.Apply(func(i int, ac *op.Account) {
		if ac.Err != nil && ac.Err != op.ErrNoCtl {
			return
		}
		c := out[i]
		*c = credsOutput{
			AccountID: c.AccountID,
			Name:      c.Name,
		}
		if out, err := km.newKey(*ac.IAM()); err == nil {
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

func newKeyMaker(user, policy arn.ARN) *keyMaker {
	name := aws.String(user.Name())
	return &keyMaker{
		iam.CreateUserInput{Path: aws.String(user.Path()), UserName: name},
		iam.AttachUserPolicyInput{PolicyArn: arn.String(policy), UserName: name},
		iam.CreateAccessKeyInput{UserName: name},
	}
}

func (m *keyMaker) newKey(c iam.IAM) (*iam.CreateAccessKeyOutput, error) {
	if _, err := c.CreateUserRequest(&m.user).Send(); err != nil {
		if !awsx.IsCode(err, iam.ErrCodeEntityAlreadyExistsException) {
			return nil, err
		}
		// Existing user path must match to create a new access key
		in := iam.GetUserInput{UserName: m.user.UserName}
		u, err := c.GetUserRequest(&in).Send()
		if err != nil {
			return nil, err
		}
		if aws.StringValue(u.User.Path) != aws.StringValue(m.user.Path) {
			return nil, errors.New("user already exists under a different path")
		}
	}
	if _, err := c.AttachUserPolicyRequest(&m.pol).Send(); err != nil {
		return nil, err
	}
	return c.CreateAccessKeyRequest(&m.key).Send()
}
