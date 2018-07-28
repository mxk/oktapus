package mock

import (
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// Role is a mock IAM role.
type Role struct {
	iam.Role
	AttachedPolicies map[string]string
	InlinePolicies   map[string]string
}

// RoleRouter handles IAM role API calls.
type RoleRouter map[string]*Role

// Route implements the Router interface.
func (r RoleRouter) Route(q *aws.Request) bool {
	switch q.Params.(type) {
	case *iam.AttachRolePolicyInput:
		r.attachRolePolicy(q)
	case *iam.CreateRoleInput:
		r.createRole(q)
	case *iam.DeleteRoleInput:
		r.deleteRole(q)
	case *iam.DeleteRolePolicyInput:
		r.deleteRolePolicy(q)
	case *iam.DetachRolePolicyInput:
		r.detachRolePolicy(q)
	case *iam.GetRoleInput:
		r.getRole(q)
	case *iam.ListAttachedRolePoliciesInput:
		r.listAttachedRolePolicies(q)
	case *iam.ListRolePoliciesInput:
		r.listRolePolicies(q)
	case *iam.ListRolesInput:
		r.listRoles(q)
	case *iam.PutRolePolicyInput:
		r.putRolePolicy(q)
	case *iam.UpdateAssumeRolePolicyInput:
		r.updateAssumeRolePolicy(q)
	case *iam.UpdateRoleDescriptionInput:
		r.updateRoleDescription(q)
	default:
		return false
	}
	return true
}

func (r RoleRouter) attachRolePolicy(q *aws.Request) {
	in := q.Params.(*iam.AttachRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		if role.AttachedPolicies == nil {
			role.AttachedPolicies = make(map[string]string)
		}
		arn := aws.StringValue(in.PolicyArn)
		role.AttachedPolicies[arn] = arn[strings.LastIndexByte(arn, '/')+1:]
	}
}

func (r RoleRouter) createRole(q *aws.Request) {
	in := q.Params.(*iam.CreateRoleInput)
	name := aws.StringValue(in.RoleName)
	if _, ok := r[name]; ok {
		panic("mock: role exists: " + name)
	}
	role := &Role{Role: iam.Role{
		Arn: aws.String(RoleARN(reqAccountID(q), name)),
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

func (r RoleRouter) deleteRole(q *aws.Request) {
	in := q.Params.(*iam.DeleteRoleInput)
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

func (r RoleRouter) deleteRolePolicy(q *aws.Request) {
	in := q.Params.(*iam.DeleteRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		name := aws.StringValue(in.PolicyName)
		if _, ok := role.InlinePolicies[name]; !ok {
			panic("mock: invalid inline policy: " + name)
		}
		delete(role.InlinePolicies, name)
	}
}

func (r RoleRouter) detachRolePolicy(q *aws.Request) {
	in := q.Params.(*iam.DetachRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		arn := aws.StringValue(in.PolicyArn)
		if _, ok := role.AttachedPolicies[arn]; !ok {
			panic("mock: invalid attached policy: " + arn)
		}
		delete(role.AttachedPolicies, arn)
	}
}

func (r RoleRouter) getRole(q *aws.Request) {
	in := q.Params.(*iam.GetRoleInput)
	if role := r.get(in.RoleName, q); role != nil {
		cpy := role.Role
		q.Data.(*iam.GetRoleOutput).Role = &cpy
	}
}

func (r RoleRouter) listAttachedRolePolicies(q *aws.Request) {
	in := q.Params.(*iam.ListAttachedRolePoliciesInput)
	if role := r.get(in.RoleName, q); role != nil {
		pols := make([]iam.AttachedPolicy, 0, len(r))
		for arn, name := range role.AttachedPolicies {
			pols = append(pols, iam.AttachedPolicy{
				PolicyArn:  aws.String(arn),
				PolicyName: aws.String(name),
			})
		}
		q.Data.(*iam.ListAttachedRolePoliciesOutput).AttachedPolicies = pols
	}
}

func (r RoleRouter) listRolePolicies(q *aws.Request) {
	in := q.Params.(*iam.ListRolePoliciesInput)
	if role := r.get(in.RoleName, q); role != nil {
		names := make([]string, 0, len(r))
		for name := range role.InlinePolicies {
			names = append(names, name)
		}
		q.Data.(*iam.ListRolePoliciesOutput).PolicyNames = names
	}
}

func (r RoleRouter) listRoles(q *aws.Request) {
	prefix := aws.StringValue(q.Params.(*iam.ListRolesInput).PathPrefix)
	roles := make([]iam.Role, 0, len(r))
	for _, role := range r {
		if strings.HasPrefix(aws.StringValue(role.Path), prefix) {
			roles = append(roles, role.Role)
		}
	}
	q.Data.(*iam.ListRolesOutput).Roles = roles
}

func (r RoleRouter) putRolePolicy(q *aws.Request) {
	in := q.Params.(*iam.PutRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		if role.InlinePolicies == nil {
			role.InlinePolicies = make(map[string]string)
		}
		name := aws.StringValue(in.PolicyName)
		role.InlinePolicies[name] = aws.StringValue(in.PolicyDocument)
	}
}

func (r RoleRouter) updateAssumeRolePolicy(q *aws.Request) {
	in := q.Params.(*iam.UpdateAssumeRolePolicyInput)
	if role := r.get(in.RoleName, q); role != nil {
		role.AssumeRolePolicyDocument = in.PolicyDocument
	}
}

func (r RoleRouter) updateRoleDescription(q *aws.Request) {
	in := q.Params.(*iam.UpdateRoleDescriptionInput)
	if role := r.get(in.RoleName, q); role != nil {
		role.Description = in.Description
		cpy := role.Role
		q.Data.(*iam.UpdateRoleDescriptionOutput).Role = &cpy
	}
}

func (r RoleRouter) get(name *string, q *aws.Request) *Role {
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
