package mock

import (
	"net/http"
	"strings"

	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// User is a mock IAM user.
type User struct {
	iam.User
	AccessKeys       []*iam.AccessKeyMetadata
	AttachedPolicies map[string]string
}

// UserRouter handles IAM user API calls.
type UserRouter map[string]*User

// Route implements the Router interface.
func (r UserRouter) Route(q *aws.Request) bool {
	switch q.Params.(type) {
	case *iam.AttachUserPolicyInput:
		r.attachUserPolicy(q)
	case *iam.CreateAccessKeyInput:
		r.createAccessKey(q)
	case *iam.CreateUserInput:
		r.createUser(q)
	case *iam.DeleteAccessKeyInput:
		r.deleteAccessKey(q)
	case *iam.DeleteUserInput:
		r.deleteUser(q)
	case *iam.DetachUserPolicyInput:
		r.detachUserPolicy(q)
	case *iam.GetUserInput:
		r.getUser(q)
	case *iam.ListAccessKeysInput:
		r.listAccessKeys(q)
	case *iam.ListAttachedUserPoliciesInput:
		r.listAttachedUserPolicies(q)
	case *iam.ListUsersInput:
		r.listUsers(q)
	default:
		return false
	}
	return true
}

func (r UserRouter) attachUserPolicy(q *aws.Request) {
	in := q.Params.(*iam.AttachUserPolicyInput)
	if user := r.get(in.UserName, q); user != nil {
		if user.AttachedPolicies == nil {
			user.AttachedPolicies = make(map[string]string)
		}
		arn := aws.StringValue(in.PolicyArn)
		user.AttachedPolicies[arn] = arn[strings.LastIndexByte(arn, '/')+1:]
	}
}

func (r UserRouter) createAccessKey(q *aws.Request) {
	in := q.Params.(*iam.CreateAccessKeyInput)
	if user := r.get(in.UserName, q); user != nil {
		ak := &iam.AccessKey{
			AccessKeyId:     aws.String(AccessKeyID),
			CreateDate:      aws.Time(fast.Time()),
			SecretAccessKey: aws.String(SecretAccessKey),
			Status:          iam.StatusTypeActive,
			UserName:        in.UserName,
		}
		user.AccessKeys = append(user.AccessKeys, &iam.AccessKeyMetadata{
			AccessKeyId: ak.AccessKeyId,
			CreateDate:  ak.CreateDate,
			Status:      ak.Status,
			UserName:    ak.UserName,
		})
		q.Data.(*iam.CreateAccessKeyOutput).AccessKey = ak
	}
}

func (r UserRouter) createUser(q *aws.Request) {
	in := q.Params.(*iam.CreateUserInput)
	name := aws.StringValue(in.UserName)
	if _, ok := r[name]; ok {
		q.Error = awserr.New(iam.ErrCodeEntityAlreadyExistsException,
			"user exists: "+name, nil)
		return
	}
	user := &User{User: iam.User{
		Arn:      aws.String(UserARN(reqAccountID(q), name)),
		Path:     in.Path,
		UserName: in.UserName,
	}}
	r[name] = user
	cpy := user.User
	q.Data.(*iam.CreateUserOutput).User = &cpy
}

func (r UserRouter) deleteAccessKey(q *aws.Request) {
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

func (r UserRouter) deleteUser(q *aws.Request) {
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

func (r UserRouter) detachUserPolicy(q *aws.Request) {
	in := q.Params.(*iam.DetachUserPolicyInput)
	if user := r.get(in.UserName, q); user != nil {
		arn := aws.StringValue(in.PolicyArn)
		if _, ok := user.AttachedPolicies[arn]; !ok {
			panic("mock: invalid attached policy: " + arn)
		}
		delete(user.AttachedPolicies, arn)
	}
}

func (r UserRouter) getUser(q *aws.Request) {
	in := q.Params.(*iam.GetUserInput)
	if user := r.get(in.UserName, q); user != nil {
		cpy := user.User
		q.Data.(*iam.GetUserOutput).User = &cpy
	}
}

func (r UserRouter) listAccessKeys(q *aws.Request) {
	in := q.Params.(*iam.ListAccessKeysInput)
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]iam.AccessKeyMetadata, 0, len(r))
		for _, pol := range user.AccessKeys {
			pols = append(pols, *pol)
		}
		q.Data.(*iam.ListAccessKeysOutput).AccessKeyMetadata = pols
	}
}

func (r UserRouter) listAttachedUserPolicies(q *aws.Request) {
	in := q.Params.(*iam.ListAttachedUserPoliciesInput)
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]iam.AttachedPolicy, 0, len(r))
		for arn, name := range user.AttachedPolicies {
			pols = append(pols, iam.AttachedPolicy{
				PolicyArn:  aws.String(arn),
				PolicyName: aws.String(name),
			})
		}
		q.Data.(*iam.ListAttachedUserPoliciesOutput).AttachedPolicies = pols
	}
}

func (r UserRouter) listUsers(q *aws.Request) {
	prefix := aws.StringValue(q.Params.(*iam.ListUsersInput).PathPrefix)
	users := make([]iam.User, 0, len(r))
	for _, user := range r {
		if strings.HasPrefix(aws.StringValue(user.Path), prefix) {
			users = append(users, user.User)
		}
	}
	q.Data.(*iam.ListUsersOutput).Users = users
}

func (r UserRouter) get(name *string, q *aws.Request) *User {
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
