package mock

import (
	"net/http"
	"strings"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// User is a mock IAM user.
type User struct {
	iam.User
	AccessKeys       []*iam.AccessKeyMetadata
	AttachedPolicies map[arn.ARN]string
}

// UserRouter handles IAM user API calls.
type UserRouter map[string]*User

// Route implements the Router interface.
func (r UserRouter) Route(q *Request) bool { return RouteMethod(r, q) }

func (r UserRouter) AttachUserPolicy(q *Request, in *iam.AttachUserPolicyInput) {
	if user := r.get(in.UserName, q); user != nil {
		if user.AttachedPolicies == nil {
			user.AttachedPolicies = make(map[arn.ARN]string)
		}
		pol := arn.Value(in.PolicyArn)
		user.AttachedPolicies[pol] = pol.Name()
	}
}

func (r UserRouter) CreateAccessKey(q *Request, in *iam.CreateAccessKeyInput) {
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

func (r UserRouter) CreateUser(q *Request, in *iam.CreateUserInput) {
	name := aws.StringValue(in.UserName)
	if _, ok := r[name]; ok {
		q.Error = awserr.New(iam.ErrCodeEntityAlreadyExistsException,
			"user exists: "+name, nil)
		return
	}
	path := aws.StringValue(in.Path)
	if path == "" {
		path = "/"
	}
	user := &User{User: iam.User{
		Arn:      arn.String(q.Ctx.New("iam", "user/", name).WithPath(path)),
		Path:     in.Path,
		UserName: in.UserName,
	}}
	r[name] = user
	cpy := user.User
	q.Data.(*iam.CreateUserOutput).User = &cpy
}

func (r UserRouter) DeleteAccessKey(q *Request, in *iam.DeleteAccessKeyInput) {
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

func (r UserRouter) DeleteUser(q *Request, in *iam.DeleteUserInput) {
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

func (r UserRouter) DetachUserPolicy(q *Request, in *iam.DetachUserPolicyInput) {
	if user := r.get(in.UserName, q); user != nil {
		pol := arn.Value(in.PolicyArn)
		if _, ok := user.AttachedPolicies[pol]; !ok {
			panic("mock: invalid attached policy: " + pol)
		}
		delete(user.AttachedPolicies, pol)
	}
}

func (r UserRouter) GetUser(q *Request, in *iam.GetUserInput) {
	if user := r.get(in.UserName, q); user != nil {
		cpy := user.User
		q.Data.(*iam.GetUserOutput).User = &cpy
	}
}

func (r UserRouter) ListAccessKeys(q *Request, in *iam.ListAccessKeysInput) {
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]iam.AccessKeyMetadata, 0, len(r))
		for _, pol := range user.AccessKeys {
			pols = append(pols, *pol)
		}
		q.Data.(*iam.ListAccessKeysOutput).AccessKeyMetadata = pols
	}
}

func (r UserRouter) ListAttachedUserPolicies(q *Request, in *iam.ListAttachedUserPoliciesInput) {
	if user := r.get(in.UserName, q); user != nil {
		pols := make([]iam.AttachedPolicy, 0, len(r))
		for pol, name := range user.AttachedPolicies {
			pols = append(pols, iam.AttachedPolicy{
				PolicyArn:  arn.String(pol),
				PolicyName: aws.String(name),
			})
		}
		q.Data.(*iam.ListAttachedUserPoliciesOutput).AttachedPolicies = pols
	}
}

func (r UserRouter) ListUsers(q *Request, in *iam.ListUsersInput) {
	prefix := aws.StringValue(in.PathPrefix)
	users := make([]iam.User, 0, len(r))
	for _, user := range r {
		if strings.HasPrefix(aws.StringValue(user.Path), prefix) {
			users = append(users, user.User)
		}
	}
	q.Data.(*iam.ListUsersOutput).Users = users
}

func (r UserRouter) get(name *string, q *Request) *User {
	if name == nil {
		name = aws.String("")
	} else if user := r[*name]; user != nil {
		return user
	}
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "unknown user: "+(*name), nil)
	q.Error = awserr.NewRequestFailure(err, http.StatusNotFound, "")
	return nil
}
