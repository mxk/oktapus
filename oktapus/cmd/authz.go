package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var authzCli = register(&cli.Info{
	Name:    "authz",
	Usage:   "[options] account-spec role-name [role-name ...]",
	Summary: "Authorize account access",
	MinArgs: 2,
	New:     func() cli.Cmd { return &authzCmd{} },
})

type authzCmd struct {
	OutFmt
	Desc      *string `flag:"Set role <description>"`
	Policy    string  `flag:"Set attached role policy <ARN> (default is AdministratorAccess)"`
	Principal string  `flag:"Set principal <ARN> for AssumeRole policy (default is current user or gateway account)"`
	Tmp       bool    `flag:"Delete this role automatically when the account is freed"`
	Spec      string
	Roles     []string
}

func (cmd *authzCmd) Info() *cli.Info { return authzCli }

func (cmd *authzCmd) Help(w *cli.Writer) {
	w.Text(`
	Authorize account access by creating or updating IAM roles.

	By default, new roles are granted admin access with AssumeRole principal
	derived from your current identity and new role name. Use an explicit
	-principal to grant access to a role/user in another account. The following
	command allows user1@example.com to access all accounts currently owned by
	you via the same gateway:

	  oktapus authz owner=me user1@example.com

	If the role already exists, the new AssumeRole principal is added without
	any other changes, provided that the role path is the same.

	Principal examples:

	  User:
	    arn:aws:iam::AWS-account-ID:user/user-name

	  Role:
	    arn:aws:iam::AWS-account-ID:role/role-name

	  Assumed role:
	    arn:aws:sts::AWS-account-ID:assumed-role/role-name/role-session-name
	`)
	accountSpecHelp(w)
}

func (cmd *authzCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *authzCmd) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec = args[0]
	cmd.Roles = args[1:]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *authzCmd) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}

	// Validate principal
	user := ctx.Gateway().Ident().ARN
	if cmd.Principal == "" {
		if t := user.Type(); t != "user" && t != "assumed-role" {
			return nil, errors.New("-principal required")
		}
	} else if !awsx.IsAccountID(cmd.Principal) &&
		!arn.ARN(cmd.Principal).Valid() {
		return nil, fmt.Errorf("invalid principal %q", cmd.Principal)
	}

	// Create API call inputs
	roles := make([]*role, 0, len(cmd.Roles))
	for _, r := range cmd.Roles {
		roles = append(roles, cmd.newRole(r, user))
	}

	// Execute
	out := make([]*roleOutput, 0, len(acs)*len(roles))
	ch := make(chan *roleOutput)
	var mu sync.Mutex
	mu.Lock()
	go func() {
		defer mu.Unlock()
		for r := range ch {
			out = append(out, r)
		}
	}()
	acs.Apply(func(_ int, ac *op.Account) {
		if ac.Err != nil {
			ch <- &roleOutput{
				AccountID: ac.ID,
				Name:      ac.Name,
				Result:    "ERROR: " + explainError(ac.Err),
			}
			return
		}
		c := ac.IAM()
		for _, r := range roles {
			create, err := r.createOrUpdate(c)
			out := &roleOutput{
				AccountID: ac.ID,
				Name:      ac.Name,
				Path:      *r.create.Path,
				Role:      *r.create.RoleName,
			}
			if err != nil {
				out.Result = "ERROR: " + explainError(err)
			} else if create {
				out.Result = "CREATED"
			} else {
				out.Result = "UPDATED"
			}
			ch <- out
		}
	})
	close(ch)
	mu.Lock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Name < b.Name || (a.Name == b.Name && a.Role < b.Role)
	})
	return out, nil
}

const adminAccess = arn.ARN("arn:aws:iam::aws:policy/AdministratorAccess")

func (cmd *authzCmd) newRole(pathName string, user arn.ARN) *role {
	r := (arn.Base + "role/").WithPathName(pathName)
	path, name := r.Path(), r.Name()
	if cmd.Tmp {
		path = op.TmpIAMPath + path[1:]
	} else if strings.IndexByte(pathName, '/') == -1 {
		path = op.IAMPath
	}
	roleName := aws.String(name)

	principal := cmd.Principal
	if principal == "" {
		principal = string(user.WithName(name))
	}
	assumeRolePolicy := iamx.AssumeRolePolicy(principal)

	policy := cmd.Policy
	if policy == "" {
		policy = string(adminAccess.WithPartition(user.Partition()))
	}
	return &role{
		get: iam.GetRoleInput{RoleName: roleName},
		create: iam.CreateRoleInput{
			AssumeRolePolicyDocument: assumeRolePolicy.Doc(),
			Description:              cmd.Desc,
			Path:                     aws.String(path),
			RoleName:                 roleName,
		},
		attach: iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policy),
			RoleName:  roleName,
		},
		assume: assumeRolePolicy.Statement[0],
	}
}

type roleOutput struct{ AccountID, Name, Path, Role, Result string }

type role struct {
	get    iam.GetRoleInput
	create iam.CreateRoleInput
	attach iam.AttachRolePolicyInput
	assume *iamx.Statement
}

func (r *role) createOrUpdate(c iamx.Client) (create bool, err error) {
	out, err := c.GetRoleRequest(&r.get).Send()
	if err != nil {
		if !awsx.IsCode(err, iam.ErrCodeNoSuchEntityException) {
			return false, err
		}
		attachPol := aws.StringValue(r.attach.PolicyArn) != ""
		_, err = c.CreateRoleRequest(&r.create).Send()
		if err == nil && attachPol {
			_, err = c.AttachRolePolicyRequest(&r.attach).Send()
		}
		return true, err
	}

	// Existing role must have an identical path
	curPath := aws.StringValue(out.Role.Path)
	newPath := aws.StringValue(r.create.Path)
	if curPath != newPath {
		return false, fmt.Errorf("existing role path mismatch (cur=%q, new=%q)",
			curPath, newPath)
	}

	// Merge AssumeRole policy
	pol, err := iamx.ParsePolicy(out.Role.AssumeRolePolicyDocument)
	if err != nil {
		return false, err
	}
	for _, s := range pol.Statement {
		if s.Effect != "Allow" || s.NotPrincipal != nil ||
			len(s.NotAction) != 0 || len(s.Resource) != 0 ||
			len(s.NotResource) != 0 || len(s.Condition) != 0 {
			continue
		}
		// Duplicates get merged by AWS
		s.Principal.AWS = append(s.Principal.AWS, r.assume.Principal.AWS...)
		goto update
	}
	pol.Statement = append(pol.Statement, r.assume)

	// Update AssumeRole policy
update:
	in := iam.UpdateAssumeRolePolicyInput{
		PolicyDocument: pol.Doc(),
		RoleName:       out.Role.RoleName,
	}
	_, err = c.UpdateAssumeRolePolicyRequest(&in).Send()
	return false, err
}
