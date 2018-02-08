package cmd

import (
	"bufio"
	"encoding/gob"
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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

		By default, this command grants admin access and sets the AssumeRole
		policy principal to your current gateway account. The following command
		allows user1@example.com to access all accounts currently owned by you:

		  oktapus authz owner=me user1@example.com

		Use -principal to specify another gateway account by ID, name, or ARN.
		If the role already exists, the principal is added without any other
		changes, provided that the role path is the same.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *authz) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	op.StringPtrVar(fs, &cmd.Desc, "desc",
		"Set role description")
	fs.StringVar(&cmd.Policy, "policy",
		"arn:aws:iam::aws:policy/AdministratorAccess",
		"Set attached role policy `ARN`")
	fs.StringVar(&cmd.Principal, "principal", "",
		"Override default principal `ARN` for AssumeRole policy")
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

	// Create AssumeRole policy document
	if cmd.Principal == "" {
		cmd.Principal = ctx.AWS().Ident().AccountID
	} else if !op.IsAWSAccountID(cmd.Principal) &&
		!strings.HasPrefix(cmd.Principal, "arn:") {
		for _, ac := range ctx.All {
			if ac.Name == cmd.Principal {
				cmd.Principal = ac.ID
				goto valid
			}
		}
		return nil, fmt.Errorf("invalid principal %q", cmd.Principal)
	valid:
	}
	assumeRolePolicy := op.NewAssumeRolePolicy(cmd.Principal)
	assumeRolePolicyDoc := assumeRolePolicy.Doc()

	// Create API call inputs
	roles := make([]*role, len(cmd.Roles))
	for i, r := range cmd.Roles {
		path, name := op.SplitPath(r)
		if cmd.Tmp {
			path = op.TmpIAMPath + path[1:]
		}
		roleName := aws.String(name)
		roles[i] = &role{
			get: iam.GetRoleInput{RoleName: roleName},
			create: iam.CreateRoleInput{
				AssumeRolePolicyDocument: assumeRolePolicyDoc,
				Description:              cmd.Desc,
				Path:                     aws.String(path),
				RoleName:                 roleName,
			},
			attach: iam.AttachRolePolicyInput{
				PolicyArn: aws.String(cmd.Policy),
				RoleName:  roleName,
			},
			assume: assumeRolePolicy.Statement[0],
		}
	}

	// Execute
	var mu sync.Mutex
	mu.Lock()
	out := make([]*roleOutput, 0, len(acs)*len(roles))
	ch := make(chan *roleOutput)
	go func() {
		defer mu.Unlock()
		for r := range ch {
			out = append(out, r)
		}
	}()
	acs.Apply(func(ac *op.Account) {
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

type roleOutput struct{ AccountID, Name, Role, Result string }

type role struct {
	get    iam.GetRoleInput
	create iam.CreateRoleInput
	attach iam.AttachRolePolicyInput
	assume *op.Statement
}

func (r *role) createOrUpdate(c iamiface.IAMAPI) (create bool, err error) {
	out, err := c.GetRole(&r.get)
	if err != nil {
		e, ok := err.(awserr.Error)
		if !ok || e.Code() != iam.ErrCodeNoSuchEntityException {
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
