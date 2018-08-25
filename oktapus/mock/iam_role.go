package mock

import (
	"net/http"
	"strings"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// Role is a mock IAM role.
type Role struct {
	iam.Role
	AttachedPolicies map[arn.ARN]string
	InlinePolicies   map[string]string
}

// RoleRouter handles IAM role API calls.
type RoleRouter map[string]*Role

// Route implements the Router interface.
func (r RoleRouter) Route(q *Request) bool { return RouteMethod(r, q) }

func (r RoleRouter) AttachRolePolicy(q *Request, in *iam.AttachRolePolicyInput) {
	if role := r.get(in.RoleName, q); role != nil {
		if role.AttachedPolicies == nil {
			role.AttachedPolicies = make(map[arn.ARN]string)
		}
		pol := arn.Value(in.PolicyArn)
		role.AttachedPolicies[pol] = pol.Name()
	}
}

func (r RoleRouter) CreateRole(q *Request, in *iam.CreateRoleInput) {
	name := aws.StringValue(in.RoleName)
	if _, ok := r[name]; ok {
		panic("mock: role exists: " + name)
	}
	path := aws.StringValue(in.Path)
	if path == "" {
		path = "/"
	}
	role := &Role{Role: iam.Role{
		Arn:                      arn.String(q.Ctx.New("iam", "role/", name).WithPath(path)),
		AssumeRolePolicyDocument: in.AssumeRolePolicyDocument,
		Description:              in.Description,
		Path:                     in.Path,
		RoleName:                 in.RoleName,
	}}
	r[name] = role
	cpy := role.Role
	cpy.Description = nil // Match AWS behavior
	q.Data.(*iam.CreateRoleOutput).Role = &cpy
}

func (r RoleRouter) DeleteRole(q *Request, in *iam.DeleteRoleInput) {
	if role := r.get(in.RoleName, q); role != nil {
		if len(role.AttachedPolicies) != 0 {
			panic("mock: role has attached policies")
		}
		if len(role.InlinePolicies) != 0 {
			panic("mock: role has inline policies")
		}
		delete(r, *in.RoleName)
	}
}

func (r RoleRouter) DeleteRolePolicy(q *Request, in *iam.DeleteRolePolicyInput) {
	if role := r.get(in.RoleName, q); role != nil {
		name := aws.StringValue(in.PolicyName)
		if _, ok := role.InlinePolicies[name]; !ok {
			panic("mock: invalid inline policy: " + name)
		}
		delete(role.InlinePolicies, name)
	}
}

func (r RoleRouter) DetachRolePolicy(q *Request, in *iam.DetachRolePolicyInput) {
	if role := r.get(in.RoleName, q); role != nil {
		pol := arn.Value(in.PolicyArn)
		if _, ok := role.AttachedPolicies[pol]; !ok {
			panic("mock: invalid attached policy: " + string(pol))
		}
		delete(role.AttachedPolicies, pol)
	}
}

func (r RoleRouter) GetRole(q *Request, in *iam.GetRoleInput) {
	if role := r.get(in.RoleName, q); role != nil {
		cpy := role.Role
		q.Data.(*iam.GetRoleOutput).Role = &cpy
	}
}

func (r RoleRouter) ListAttachedRolePolicies(q *Request, in *iam.ListAttachedRolePoliciesInput) {
	if role := r.get(in.RoleName, q); role != nil {
		pols := make([]iam.AttachedPolicy, 0, len(r))
		for pol, name := range role.AttachedPolicies {
			pols = append(pols, iam.AttachedPolicy{
				PolicyArn:  arn.String(pol),
				PolicyName: aws.String(name),
			})
		}
		q.Data.(*iam.ListAttachedRolePoliciesOutput).AttachedPolicies = pols
	}
}

func (r RoleRouter) ListRolePolicies(q *Request, in *iam.ListRolePoliciesInput) {
	if role := r.get(in.RoleName, q); role != nil {
		names := make([]string, 0, len(r))
		for name := range role.InlinePolicies {
			names = append(names, name)
		}
		q.Data.(*iam.ListRolePoliciesOutput).PolicyNames = names
	}
}

func (r RoleRouter) ListRoles(q *Request, in *iam.ListRolesInput) {
	prefix := aws.StringValue(in.PathPrefix)
	roles := make([]iam.Role, 0, len(r))
	for _, role := range r {
		if strings.HasPrefix(aws.StringValue(role.Path), prefix) {
			roles = append(roles, role.Role)
		}
	}
	q.Data.(*iam.ListRolesOutput).Roles = roles
}

func (r RoleRouter) PutRolePolicy(q *Request, in *iam.PutRolePolicyInput) {
	if role := r.get(in.RoleName, q); role != nil {
		if role.InlinePolicies == nil {
			role.InlinePolicies = make(map[string]string)
		}
		name := aws.StringValue(in.PolicyName)
		role.InlinePolicies[name] = aws.StringValue(in.PolicyDocument)
	}
}

func (r RoleRouter) UpdateAssumeRolePolicy(q *Request, in *iam.UpdateAssumeRolePolicyInput) {
	if role := r.get(in.RoleName, q); role != nil {
		role.AssumeRolePolicyDocument = in.PolicyDocument
	}
}

func (r RoleRouter) UpdateRoleDescription(q *Request, in *iam.UpdateRoleDescriptionInput) {
	if role := r.get(in.RoleName, q); role != nil {
		role.Description = in.Description
		cpy := role.Role
		q.Data.(*iam.UpdateRoleDescriptionOutput).Role = &cpy
	}
}

func (r RoleRouter) get(name *string, q *Request) *Role {
	if name == nil {
		name = aws.String("")
	} else if role := r[*name]; role != nil {
		return role
	}
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "unknown role: "+(*name), nil)
	q.Error = awserr.NewRequestFailure(err, http.StatusNotFound, "")
	return nil
}
