package cmd

import (
	"errors"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var credsCli = cli.Main.Add(&cli.Info{
	Name:    "creds",
	Usage:   "[options] account-spec",
	Summary: "Get account credentials",
	MinArgs: 1,
	MaxArgs: 1,
	New: func() cli.Cmd {
		return &credsCmd{
			Dur:    5 * time.Minute,
			Policy: "AdministratorAccess",
		}
	},
})

type credsCmd struct {
	OutFmt
	Dur    time.Duration `flag:"Minimum credential validity <duration>"`
	User   string        `flag:"Create new access keys for the <name>d IAM user"`
	Policy string        `flag:"Attach managed policy <name> or ARN to user"`
	Tmp    bool          `flag:"Delete user automatically when the account is freed"`
	Spec   string
}

func (*credsCmd) Info() *cli.Info { return credsCli }

func (*credsCmd) Help(w *cli.Writer) {
	w.Text(`
	Get account credentials.

	By default, this command returns temporary credentials for all accounts that
	match the spec. Credentials are cached and renewed automatically when they
	are set to expire within 5 minutes. You can increase this duration with -dur
	(e.g. -dur=30m) or force unconditional renewal with a negative duration
	(e.g. -dur=-1s).

	If -user is specified, the command creates long-term IAM access keys for new
	or existing IAM users. If -tmp is specified, the users will be automatically
	deleted when the account is freed. If -policy is empty, user policies are
	not modified (default is to attach AdministratorAccess policy).
	`)
	accountSpecHelp(w)
}

func (cmd *credsCmd) Main(args []string) error {
	cmd.Spec = args[0]
	// TODO: Set non-zero exit code for any error
	return op.RunAndPrint(cmd)
}

func (cmd *credsCmd) Run(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	if cmd.User != "" {
		cmd.Dur = time.Minute
	}
	out := listCreds(acs.EnsureCreds(cmd.Dur))
	if cmd.User == "" {
		return out, nil
	}

	// Create user access keys
	user := (arn.Base + "user/").WithPathName(cmd.User)
	if cmd.Tmp {
		user = user.WithPath(op.IAMTmpPath + user.Path())
	}
	policy := iamx.ManagedPolicyARN(ctx.Ident().Partition(), cmd.Policy)
	if policy == "" && cmd.Policy != "" {
		return nil, errors.New("invalid policy name")
	}
	km := newKeyMaker(user, policy)
	acs.Map(func(i int, ac *op.Account) error {
		co := out[i]
		if co.Error != "" {
			return nil
		} else if !ac.CredsValid() {
			co.Error = explainError(op.ErrNoAccess)
			return nil
		}
		*co = credsOutput{
			Account: co.Account,
			Name:    co.Name,
		}
		if out, err := km.newKey(ac.IAM); err == nil {
			co.AccessKeyID = aws.StringValue(out.AccessKey.AccessKeyId)
			co.SecretAccessKey = aws.StringValue(out.AccessKey.SecretAccessKey)
		} else {
			co.Error = explainError(err)
		}
		return nil
	})
	return out, nil
}

// credsOutput provides account credentials to the user.
type credsOutput struct {
	Account         string
	Name            string
	Expires         expTime
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string `json:",omitempty" printer:",width=1,last"`
	Error           string `json:",omitempty"`
}

func listCreds(acs op.Accounts) []*credsOutput {
	out := make([]*credsOutput, len(acs))
	for i, ac := range acs.CredsOrErr() {
		err := ac.Err
		if ac.CredsValid() {
			err = nil
		}
		co := &credsOutput{
			Account: ac.ID,
			Name:    ac.Name,
			Error:   explainError(err),
		}
		if err == nil {
			cr, _ := ac.CredsProvider().Creds()
			co.Expires = expTime{cr.Expires}
			co.AccessKeyID = cr.AccessKeyID
			co.SecretAccessKey = cr.SecretAccessKey
			co.SessionToken = cr.SessionToken
		}
		out[i] = co
	}
	return out
}

func (o *credsOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.Account, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}

// keyMaker creates new IAM user access keys.
type keyMaker struct {
	user iam.CreateUserInput
	pol  iam.AttachUserPolicyInput
	key  iam.CreateAccessKeyInput
}

func newKeyMaker(user, policy arn.ARN) *keyMaker {
	name := aws.String(user.Name())
	var pol *string
	if policy != "" {
		pol = arn.String(policy)
	}
	return &keyMaker{
		iam.CreateUserInput{Path: aws.String(user.Path()), UserName: name},
		iam.AttachUserPolicyInput{PolicyArn: pol, UserName: name},
		iam.CreateAccessKeyInput{UserName: name},
	}
}

func (m *keyMaker) newKey(c iamx.Client) (*iam.CreateAccessKeyOutput, error) {
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
			return nil, errors.New("user path mismatch")
		}
	}
	if m.pol.PolicyArn != nil {
		if _, err := c.AttachUserPolicyRequest(&m.pol).Send(); err != nil {
			return nil, err
		}
	}
	return c.CreateAccessKeyRequest(&m.key).Send()
}
