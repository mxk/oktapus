package cmd

import (
	"fmt"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var authzCli = cli.Main.Add(&cli.Info{
	Name:    "authz",
	Usage:   "[options] account-spec principal [principal ...]",
	Summary: "Authorize role-based account access",
	MinArgs: 2,
	New:     func() cli.Cmd { return &authzCmd{Policy: "AdministratorAccess"} },
})

type authzCmd struct {
	OutFmt
	Desc       *string `flag:"Role <description>"`
	Policy     string  `flag:"Attach managed policy <name> or ARN"`
	Role       string  `flag:"Role <name> with optional path"`
	Tmp        bool    `flag:"Delete role(s) automatically when the account is freed"`
	Spec       string
	Principals []string
}

func (*authzCmd) Info() *cli.Info { return authzCli }

func (*authzCmd) Help(w *cli.Writer) {
	w.Text(`
	Authorize role-based account access.

	This command creates or updates IAM roles in each matching account, allowing
	the associated principal to assume that role. Role names are derived from
	principal names unless -role is specified, in which case only that role is
	created or updated.

	If a role with a matching name and path already exists, new principals are
	added to its AssumeRole policy without any other changes.

	Principal examples:

	  arn:aws:iam::123456789012:user/user-name
	  arn:aws:iam::123456789012:role/path/role-name
	  arn:aws:sts::123456789012:assumed-role/role-name/role-session-name
	  123456789012
	  123456789012:user/path/user-name
	  role/role-name

	The -role option is required if one of the principals is an account ID. The
	ARN prefix up to the resource may be omitted. Account ID defaults to the
	current gateway account if not specified.
	`)
	accountSpecHelp(w)
}

func (cmd *authzCmd) Main(args []string) error {
	cmd.Spec = args[0]
	cmd.Principals = args[1:]
	return op.RunAndPrint(cmd)
}

func (cmd *authzCmd) Run(ctx *op.Ctx) (interface{}, error) {
	attachPolicy, err := getManagedPolicy(ctx.Ident().Partition(), cmd.Policy)
	if err != nil {
		return nil, err
	} else if err := cmd.checkPrincipals(ctx.Ident().Ctx()); err != nil {
		return nil, err
	}

	// Create API call inputs
	var roles []*roleAuthz
	if cmd.Role == "" {
		roles = make([]*roleAuthz, len(cmd.Principals))
		dup := make(map[string]bool, len(cmd.Principals))
		path, _, _ := splitPathName("", cmd.Tmp)
		for i, p := range cmd.Principals {
			name := arn.ARN(p).Name()
			if dup[name] {
				return nil, fmt.Errorf("duplicate role name %q", name)
			}
			dup[name] = true
			roles[i] = newRoleAuthz(path, name, attachPolicy, p)
		}
	} else {
		path, name, err := splitPathName(cmd.Role, cmd.Tmp)
		if err != nil {
			return nil, err
		}
		roles = []*roleAuthz{newRoleAuthz(path, name, attachPolicy,
			cmd.Principals...)}
	}
	if cmd.Desc != nil {
		for _, r := range roles {
			r.create.Description = cmd.Desc
		}
	}

	// Execute
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	out := make([]*roleOutput, len(acs)*len(roles))
	if len(out) == 0 {
		return nil, nil
	}
	compact := false
	acs.EnsureCreds(minDur).Map(func(i int, ac *op.Account) error {
		i *= len(roles)
		out := out[i : i+len(roles)]
		if !ac.CredsValid() {
			out[0] = &roleOutput{
				Account: ac.ID,
				Name:    ac.Name,
				Result:  "ERROR: " + explainError(ac.Err),
			}
			compact = true
			return nil
		}
		return fast.ForEachIO(len(roles), func(i int) error {
			role, created, err := roles[i].exec(ac.IAM)
			ro := &roleOutput{
				Account: ac.ID,
				Name:    ac.Name,
			}
			if role != nil {
				ro.Role = aws.StringValue(role.Arn)
			}
			if err != nil {
				ro.Result = "ERROR: " + explainError(err)
			} else if created {
				ro.Result = "CREATED"
			} else {
				ro.Result = "UPDATED"
			}
			out[i] = ro
			return nil
		})
	})
	if compact {
		i := 0
		for _, r := range out {
			if r != nil {
				out[i] = r
				i++
			}
		}
		out = out[:i]
	}
	return out, nil
}

type roleOutput struct{ Account, Name, Role, Result string }

func (cmd *authzCmd) checkPrincipals(ctx arn.Ctx) error {
	identAccount := ctx.Account
	for i, p := range cmd.Principals {
		if account.IsID(p) {
			if cmd.Role == "" {
				return cli.Error("-role required for account ID principal")
			}
			continue
		}
		r := arn.ARN(p)
		if !r.Valid() {
			if len(r) > 12 && r[12] == ':' && account.IsID(p[:12]) {
				r = arn.Base[:len(arn.Base)-1] + r
			} else {
				r = arn.Base + r
			}
		}
		if ctx.Account = r.Account(); ctx.Account == "" {
			ctx.Account = identAccount
		}
		switch t := r.Type(); t {
		case "user", "role":
			r = ctx.New("iam", t, r.Path(), r.Name())
		case "assumed-role":
			path := r.Path()
			if len(path) <= 1 {
				return fmt.Errorf("invalid role name %q in %q", path, p)
			}
			r = ctx.New("sts", t, path, r.Name())
		default:
			return fmt.Errorf("invalid principal type %q in %q", t, p)
		}
		cmd.Principals[i] = string(r)
	}
	return nil
}

// roleAuthz creates new roles and updates existing AssumeRole policies.
type roleAuthz struct {
	get    iam.GetRoleInput
	create iam.CreateRoleInput
	attach iam.AttachRolePolicyInput
	trust  *iamx.Statement
}

func newRoleAuthz(path, name string, attachPolicy arn.ARN, principals ...string) *roleAuthz {
	roleName := aws.String(name)
	assumeRolePolicy := iamx.AssumeRolePolicy(iamx.Allow, principals...)
	return &roleAuthz{
		iam.GetRoleInput{RoleName: roleName},
		iam.CreateRoleInput{
			AssumeRolePolicyDocument: assumeRolePolicy.Doc(),
			Path:                     aws.String(path),
			RoleName:                 roleName,
		},
		iam.AttachRolePolicyInput{
			PolicyArn: arn.String(attachPolicy),
			RoleName:  roleName,
		},
		assumeRolePolicy.Statement[0],
	}
}

func (r *roleAuthz) exec(c iamx.Client) (role *iam.Role, created bool, err error) {
	out, err := c.GetRoleRequest(&r.get).Send()
	if err != nil {
		if !awsx.IsCode(err, iam.ErrCodeNoSuchEntityException) {
			return nil, false, err
		}
		out, err := c.CreateRoleRequest(&r.create).Send()
		if err == nil && arn.Value(r.attach.PolicyArn) != "" {
			_, err = c.AttachRolePolicyRequest(&r.attach).Send()
		}
		return out.Role, true, err
	}

	// Existing role must have identical path
	role = out.Role
	if aws.StringValue(role.Path) != aws.StringValue(r.create.Path) {
		err = op.Error("role path mismatch")
		return
	}

	// Merge and update AssumeRole policy
	pol, err := iamx.ParsePolicy(role.AssumeRolePolicyDocument)
	if err == nil {
		appendAssumeRolePolicy(pol, r.trust)
		in := iam.UpdateAssumeRolePolicyInput{
			PolicyDocument: pol.Doc(),
			RoleName:       role.RoleName,
		}
		_, err = c.UpdateAssumeRolePolicyRequest(&in).Send()
	}
	return
}

func appendAssumeRolePolicy(p *iamx.Policy, s *iamx.Statement) {
	for _, t := range p.Statement {
		// Resources are not allowed, Principal and NotPrincipal are mutually
		// exclusive and cannot be empty.
		if t.Effect != s.Effect || t.NotPrincipal != nil ||
			!t.Action.Equal(s.Action) || len(t.Condition) != 0 {
			continue
		}
		// Duplicates get merged by AWS
		t.Principal.AWS = append(t.Principal.AWS, s.Principal.AWS...)
		return
	}
	p.Statement = append(p.Statement, s)
}
