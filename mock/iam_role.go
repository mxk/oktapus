package mock

import (
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/iam"
)

// Role is a mock IAM role.
type Role struct {
	iam.Role
	AttachedPolicies []*iam.AttachedPolicy
}

// RoleRouter handles IAM role API calls.
type RoleRouter map[string]*Role

// Route implements the Router interface.
func (r RoleRouter) Route(s *Session, q *request.Request, api string) bool {
	switch api {
	case "iam:CreateRole":
		r.createRole(q)
	case "iam:DeleteRole":
		r.deleteRole(q)
	case "iam:DetachRolePolicy":
		r.detachRolePolicy(q)
	case "iam:GetRole":
		r.getRole(q)
	case "iam:ListAttachedRolePolicies":
		r.listAttachedRolePolicies(q)
	case "iam:ListRoles":
		r.listRoles(q)
	case "iam:UpdateRoleDescription":
		r.updateRoleDescription(q)
	default:
		return false
	}
	return true
}

func (r RoleRouter) createRole(q *request.Request) {
	in := q.Params.(*iam.CreateRoleInput)
	name := aws.StringValue(in.RoleName)
	if _, ok := r[name]; ok {
		panic("mock: role exists: " + name)
	}
	role := &Role{Role: iam.Role{
		Arn: aws.String(RoleARN(getReqAccountID(q), name)),
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

func (r RoleRouter) deleteRole(q *request.Request) {
	in := q.Params.(*iam.DeleteRoleInput)
	if role := r.get(in.RoleName, q); role != nil {
		if len(role.AttachedPolicies) != 0 {
			panic("mock: role has attached policies")
		}
		delete(r, *in.RoleName)
	}
}

func (r RoleRouter) detachRolePolicy(q *request.Request) {
	in := q.Params.(*iam.DetachRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		arn := aws.StringValue(in.PolicyArn)
		for i, pol := range role.AttachedPolicies {
			if aws.StringValue(pol.PolicyArn) == arn {
				role.AttachedPolicies = append(role.AttachedPolicies[:i],
					role.AttachedPolicies[i+1:]...)
				return
			}
		}
		panic("mock: invalid policy: " + arn)
	}
}

func (r RoleRouter) getRole(q *request.Request) {
	in := q.Params.(*iam.GetRoleInput)
	if role := r.get(in.RoleName, q); role != nil {
		cpy := role.Role
		q.Data.(*iam.GetRoleOutput).Role = &cpy
	}
}

func (r RoleRouter) listAttachedRolePolicies(q *request.Request) {
	in := q.Params.(*iam.ListAttachedRolePoliciesInput)
	if role := r.get(in.RoleName, q); role != nil {
		pols := make([]*iam.AttachedPolicy, 0, len(r))
		for _, pol := range role.AttachedPolicies {
			cpy := *pol
			pols = append(pols, &cpy)
		}
		q.Data.(*iam.ListAttachedRolePoliciesOutput).AttachedPolicies = pols
	}
}

func (r RoleRouter) listRoles(q *request.Request) {
	prefix := aws.StringValue(q.Params.(*iam.ListRolesInput).PathPrefix)
	roles := make([]*iam.Role, 0, len(r))
	for _, role := range r {
		if !strings.HasPrefix(aws.StringValue(role.Path), prefix) {
			continue
		}
		cpy := role.Role
		roles = append(roles, &cpy)
	}
	q.Data.(*iam.ListRolesOutput).Roles = roles
}

func (r RoleRouter) updateRoleDescription(q *request.Request) {
	in := q.Params.(*iam.UpdateRoleDescriptionInput)
	if role := r.get(in.RoleName, q); role != nil {
		role.Description = in.Description
		cpy := role.Role
		q.Data.(*iam.UpdateRoleDescriptionOutput).Role = &cpy
	}
}

func (r RoleRouter) get(name *string, q *request.Request) *Role {
	if name != nil {
		if role := r[*name]; role != nil {
			return role
		}
	} else {
		name = aws.String("")
	}
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "Unknown role: "+(*name), nil)
	q.Error = awserr.NewRequestFailure(err, http.StatusNotFound, "")
	return nil
}
