package cmd

import (
	"bufio"
	"errors"
	"flag"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"creds"},
		Summary: "Get account credentials",
		Usage:   "[options] account-spec",
		MinArgs: 1,
		MaxArgs: 1,
		New:     func() op.Cmd { return &creds{Name: "creds"} },
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
	op.WriteHelp(w, `
		Get account credentials.

		By default, this command returns temporary credentials for all accounts
		that match the spec. These credentials are cached and are renewed only
		after expiration. You can force renewal with the -renew option.

		If you need long-term credentials, the command allows you to create an
		IAM user with an access key. If you use the -tmp option, the user will
		be automatically deleted when the account is freed.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *creds) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Renew, "renew", false,
		"Renew temporary credentials")
	fs.StringVar(&cmd.User, "user", "",
		"Get long-term credentials for the `name`d IAM user")
	fs.StringVar(&cmd.Policy, "policy", "",
		"Set user policy `ARN` (default is AdministratorAccess)")
	fs.BoolVar(&cmd.Tmp, "tmp", false,
		"Delete this user automatically when the account is freed")
}

func (cmd *creds) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *creds) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	out := listCreds(acs, cmd.Renew)
	if cmd.User == "" {
		return out, nil
	}
	user := (awsx.NilARN + "user/").WithPathName(cmd.User)
	if cmd.Tmp {
		user = user.WithPath(op.TmpIAMPath + user.Path())
	}
	policy := awsx.ARN(cmd.Policy)
	if policy == "" {
		part := ctx.Gateway().Ident().UserARN.Partition()
		policy = awsx.AdminAccess.WithPartition(part)
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
		if out, err := km.newKey(ac.IAM()); err == nil {
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

func newKeyMaker(user, policy awsx.ARN) *keyMaker {
	name := aws.String(user.Name())
	return &keyMaker{
		iam.CreateUserInput{Path: aws.String(user.Path()), UserName: name},
		iam.AttachUserPolicyInput{PolicyArn: policy.Str(), UserName: name},
		iam.CreateAccessKeyInput{UserName: name},
	}
}

func (m *keyMaker) newKey(c iamiface.IAMAPI) (*iam.CreateAccessKeyOutput, error) {
	if _, err := c.CreateUser(&m.user); err != nil {
		if !awsx.IsCode(err, iam.ErrCodeEntityAlreadyExistsException) {
			return nil, err
		}
		// Existing user path must match to create a new access key
		in := iam.GetUserInput{UserName: m.user.UserName}
		u, err := c.GetUser(&in)
		if err != nil {
			return nil, err
		}
		if aws.StringValue(u.User.Path) != aws.StringValue(m.user.Path) {
			return nil, errors.New("user already exists under a different path")
		}
	}
	if _, err := c.AttachUserPolicy(&m.pol); err != nil {
		return nil, err
	}
	return c.CreateAccessKey(&m.key)
}
