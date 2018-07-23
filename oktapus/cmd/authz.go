package cmd

import (
	"bufio"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"authz"},
		Summary: "Authorize account access",
		Usage:   "[options] account-spec role-name [role-name ...]",
		MinArgs: 2,
		New:     func() op.Cmd { return &authz{Name: "authz"} },
	})
	gob.Register([]*roleOutput{})
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
		Authorize account access by creating or updating IAM roles.

		By default, new roles are granted admin access with AssumeRole principal
		derived from your current identity and new role name. Use an explicit
		-principal to grant access to a role/user in another account. The
		following command allows user1@example.com to access all accounts
		currently owned by you via the same gateway:

		  oktapus authz owner=me user1@example.com

		If the role already exists, the new AssumeRole principal is added
		without any other changes, provided that the role path is the same.

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

func (cmd *authz) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	op.StringPtrVar(fs, &cmd.Desc, "desc",
		"Set role `description`")
	fs.StringVar(&cmd.Policy, "policy", "",
		"Set attached role policy `ARN` (default is AdministratorAccess)")
	fs.StringVar(&cmd.Principal, "principal", "",
		"Set principal `ARN` for AssumeRole policy (default is current user or gateway account)")
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

	// Validate principal
	user := ctx.Gateway().Ident().UserARN
	if cmd.Principal == "" {
		if t := user.Type(); t != "user" && t != "assumed-role" {
			return nil, errors.New("-principal required")
		}
	} else if !awsx.IsAccountID(cmd.Principal) &&
		!awsx.ARN(cmd.Principal).Valid() {
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

func (cmd *authz) newRole(pathName string, user awsx.ARN) *role {
	arn := (awsx.NilARN + "role/").WithPathName(pathName)
	path, name := arn.Path(), arn.Name()
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
	assumeRolePolicy := op.NewAssumeRolePolicy(principal)

	policy := cmd.Policy
	if policy == "" {
		policy = string(awsx.AdminAccess.WithPartition(user.Partition()))
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
	assume *op.Statement
}

func (r *role) createOrUpdate(c iamiface.IAMAPI) (create bool, err error) {
	out, err := c.GetRole(&r.get)
	if err != nil {
		if !awsx.IsCode(err, iam.ErrCodeNoSuchEntityException) {
			return false, err
		}
		attachPol := aws.StringValue(r.attach.PolicyArn) != ""
		if _, err = c.CreateRole(&r.create); err == nil && attachPol {
			_, err = c.AttachRolePolicy(&r.attach)
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
	pol, err := op.ParsePolicy(out.Role.AssumeRolePolicyDocument)
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
	_, err = c.UpdateAssumeRolePolicy(&in)
	return false, err
}
