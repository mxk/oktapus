package mock

import (
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/iam"
)

// User is a mock IAM user.
type User struct {
	iam.User
	AccessKeys       []*iam.AccessKeyMetadata
	AttachedPolicies []*iam.AttachedPolicy
}

// UserRouter handles IAM user API calls.
type UserRouter map[string]*User

// Route implements the Router interface.
func (r UserRouter) Route(s *Session, q *request.Request, api string) bool {
	switch api {
	case "iam:CreateUser":
		r.createUser(q)
	case "iam:DeleteAccessKey":
		r.deleteAccessKey(q)
	case "iam:DeleteUser":
		r.deleteUser(q)
	case "iam:DetachUserPolicy":
		r.detachUserPolicy(q)
	case "iam:GetUser":
		r.getUser(q)
	case "iam:ListAccessKeys":
		r.listAccessKeys(q)
	case "iam:ListAttachedUserPolicies":
		r.listAttachedUserPolicies(q)
	case "iam:ListUsers":
		r.listUsers(q)
	default:
		return false
	}
	return true
}

func (r UserRouter) createUser(q *request.Request) {
	in := q.Params.(*iam.CreateUserInput)
	name := aws.StringValue(in.UserName)
	if _, ok := r[name]; ok {
		panic("mock: user exists: " + name)
	}
	user := &User{User: iam.User{
		Arn:      aws.String(UserARN(getReqAccountID(q), name)),
		Path:     in.Path,
		UserName: in.UserName,
	}}
	r[name] = user
	cpy := user.User
	q.Data.(*iam.CreateUserOutput).User = &cpy
}

func (r UserRouter) deleteAccessKey(q *request.Request) {
	in := q.Params.(*iam.DeleteAccessKeyInput)
	if user := r.get(in.UserName, q); user != nil {
		id := aws.StringValue(in.AccessKeyId)
		for i, k := range user.AccessKeys {
			if aws.StringValue(k.AccessKeyId) == id {
				user.AccessKeys = append(user.AccessKeys[:i],
					user.AccessKeys[i+1:]...)
				return
			}
		}
		panic("mock: invalid access key id: " + id)
	}
}

func (r UserRouter) deleteUser(q *request.Request) {
	in := q.Params.(*iam.DeleteUserInput)
	if user := r.get(in.UserName, q); user != nil {
		if len(user.AttachedPolicies) != 0 {
			panic("mock: user has attached policies")
		}
		if len(user.AccessKeys) != 0 {
			panic("mock: user has access keys")
		}
		delete(r, *in.UserName)
	}
}

func (r UserRouter) detachUserPolicy(q *request.Request) {
	in := q.Params.(*iam.DetachUserPolicyInput)
	if user := r.get(in.UserName, q); user != nil {
		arn := aws.StringValue(in.PolicyArn)
		for i, pol := range user.AttachedPolicies {
			if aws.StringValue(pol.PolicyArn) == arn {
				user.AttachedPolicies = append(user.AttachedPolicies[:i],
					user.AttachedPolicies[i+1:]...)
				return
			}
		}
		panic("mock: invalid policy: " + arn)
	}
}

func (r UserRouter) getUser(q *request.Request) {
	in := q.Params.(*iam.GetUserInput)
	if user := r.get(in.UserName, q); user != nil {
		cpy := user.User
		q.Data.(*iam.GetUserOutput).User = &cpy
	}
}

func (r UserRouter) listAccessKeys(q *request.Request) {
	in := q.Params.(*iam.ListAccessKeysInput)
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]*iam.AccessKeyMetadata, 0, len(r))
		for _, pol := range user.AccessKeys {
			cpy := *pol
			pols = append(pols, &cpy)
		}
		q.Data.(*iam.ListAccessKeysOutput).AccessKeyMetadata = pols
	}
}

func (r UserRouter) listAttachedUserPolicies(q *request.Request) {
	in := q.Params.(*iam.ListAttachedUserPoliciesInput)
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]*iam.AttachedPolicy, 0, len(r))
		for _, pol := range user.AttachedPolicies {
			cpy := *pol
			pols = append(pols, &cpy)
		}
		q.Data.(*iam.ListAttachedUserPoliciesOutput).AttachedPolicies = pols
	}
}

func (r UserRouter) listUsers(q *request.Request) {
	prefix := aws.StringValue(q.Params.(*iam.ListUsersInput).PathPrefix)
	users := make([]*iam.User, 0, len(r))
	for _, user := range r {
		if !strings.HasPrefix(aws.StringValue(user.Path), prefix) {
			continue
		}
		cpy := user.User
		users = append(users, &cpy)
	}
	q.Data.(*iam.ListUsersOutput).Users = users
}

func (r UserRouter) get(name *string, q *request.Request) *User {
	if name != nil {
		if user := r[*name]; user != nil {
			return user
		}
	} else {
		name = aws.String("")
	}
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "Unknown user: "+(*name), nil)
	q.Error = awserr.NewRequestFailure(err, http.StatusNotFound, "")
	return nil
}
