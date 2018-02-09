package op

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	orgsiface "github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
)

// TmpIAMPath is a path for temporary users and roles.
const TmpIAMPath = IAMPath + "tmp/"

// SplitPath splits a string in the format "[[/]path/]name" into its components.
// The path always begins and ends with a slash.
func SplitPath(s string) (path, name string) {
	i := strings.LastIndexByte(s, '/')
	if path, name = s[:i+1], s[i+1:]; path == "" {
		return "/", name
	} else if path[0] != '/' {
		path = "/" + path
	}
	return path, name
}

// CreateAccountResult contains the values returned by createAccount. If err is
// not nil, Account will contain the original name from CreateAccountInput.
type CreateAccountResult struct {
	*orgs.Account
	Err error
}

// CreateAccounts creates multiple accounts concurrently.
func CreateAccounts(c orgsiface.OrganizationsAPI, in <-chan *orgs.CreateAccountInput) <-chan CreateAccountResult {
	workers := 5 // Only 5 accounts may be created at the same time
	var wg sync.WaitGroup
	wg.Add(workers)
	out := make(chan CreateAccountResult)
	for ; workers > 0; workers-- {
		go func() {
			defer wg.Done()
			for v := range in {
				ac, err := createAccount(c, v)
				if err != nil {
					// TODO: Retry if err is too many account creation ops
					if ac == nil {
						ac = &orgs.Account{Name: v.AccountName, Email: v.Email}
					} else if ac.Name == nil {
						ac.Name = v.AccountName
					}
				}
				out <- CreateAccountResult{ac, err}
			}
		}()
	}
	go func() {
		defer close(out)
		wg.Wait()
	}()
	return out
}

var sleep = time.Sleep

// createAccount creates a new account in the organization.
func createAccount(c orgsiface.OrganizationsAPI, in *orgs.CreateAccountInput) (*orgs.Account, error) {
	out, err := c.CreateAccount(in)
	if err != nil {
		return nil, err
	}
	s := out.CreateAccountStatus
	reqID := orgs.DescribeCreateAccountStatusInput{
		CreateAccountRequestId: s.Id,
	}
	for {
		switch aws.StringValue(s.State) {
		case orgs.CreateAccountStateInProgress:
			sleep(time.Second)
			out, err := c.DescribeCreateAccountStatus(&reqID)
			if err != nil {
				return nil, err
			}
			s = out.CreateAccountStatus
		case orgs.CreateAccountStateSucceeded:
			in := orgs.DescribeAccountInput{AccountId: s.AccountId}
			out, err := c.DescribeAccount(&in)
			return out.Account, err
		default:
			return nil, awserr.New(aws.StringValue(s.FailureReason),
				"account creation failed", nil)
		}
	}
}

// DelTmpUsers deletes all users under the temporary IAM path.
func DelTmpUsers(c iamiface.IAMAPI) error {
	var users []string
	in := iam.ListUsersInput{PathPrefix: aws.String(TmpIAMPath)}
	pager := func(out *iam.ListUsersOutput, lastPage bool) bool {
		for _, u := range out.Users {
			users = append(users, aws.StringValue(u.UserName))
		}
		return true
	}
	if err := c.ListUsersPages(&in, pager); err != nil {
		return err
	}
	return goForEach(users, func(user interface{}) error {
		return delUser(c, user.(string))
	})
}

// delUser deletes the specified user, ensuring that all prerequisites for
// deletion are met.
func delUser(c iamiface.IAMAPI, user string) error {
	if err := detachUserPolicies(c, user); err != nil {
		return err
	} else if err = delAccessKeys(c, user); err != nil {
		return err
	}
	in := iam.DeleteUserInput{UserName: aws.String(user)}
	_, err := c.DeleteUser(&in)
	return err
}

// delAccessKeys deletes all user access keys.
func delAccessKeys(c iamiface.IAMAPI, user string) error {
	var ids []string
	in := iam.ListAccessKeysInput{UserName: aws.String(user)}
	pager := func(out *iam.ListAccessKeysOutput, lastPage bool) bool {
		for _, key := range out.AccessKeyMetadata {
			ids = append(ids, aws.StringValue(key.AccessKeyId))
		}
		return true
	}
	if err := c.ListAccessKeysPages(&in, pager); err != nil {
		return err
	}
	return goForEach(ids, func(id interface{}) error {
		in := iam.DeleteAccessKeyInput{
			AccessKeyId: aws.String(id.(string)),
			UserName:    aws.String(user),
		}
		_, err := c.DeleteAccessKey(&in)
		return err
	})
}

// detachUserPolicies detaches all user policies.
func detachUserPolicies(c iamiface.IAMAPI, user string) error {
	var arns []string
	in := iam.ListAttachedUserPoliciesInput{UserName: aws.String(user)}
	pager := func(out *iam.ListAttachedUserPoliciesOutput, lastPage bool) bool {
		for _, pol := range out.AttachedPolicies {
			arns = append(arns, aws.StringValue(pol.PolicyArn))
		}
		return true
	}
	if err := c.ListAttachedUserPoliciesPages(&in, pager); err != nil {
		return err
	}
	return goForEach(arns, func(arn interface{}) error {
		in := iam.DetachUserPolicyInput{
			PolicyArn: aws.String(arn.(string)),
			UserName:  aws.String(user),
		}
		_, err := c.DetachUserPolicy(&in)
		return err
	})
}

// DelTmpRoles deletes all roles under the temporary IAM path.
func DelTmpRoles(c iamiface.IAMAPI) error {
	var roles []string
	in := iam.ListRolesInput{PathPrefix: aws.String(TmpIAMPath)}
	pager := func(out *iam.ListRolesOutput, lastPage bool) bool {
		for _, r := range out.Roles {
			roles = append(roles, aws.StringValue(r.RoleName))
		}
		return true
	}
	if err := c.ListRolesPages(&in, pager); err != nil {
		return err
	}
	return goForEach(roles, func(role interface{}) error {
		return delRole(c, role.(string))
	})
}

// delRole deletes the specified role, ensuring that all prerequisites for
// deletion are met.
func delRole(c iamiface.IAMAPI, role string) error {
	if err := detachRolePolicies(c, role); err != nil {
		return err
	}
	in := iam.DeleteRoleInput{RoleName: aws.String(role)}
	_, err := c.DeleteRole(&in)
	return err
}

// detachRolePolicies detaches all role policies.
func detachRolePolicies(c iamiface.IAMAPI, role string) error {
	var arns []string
	in := iam.ListAttachedRolePoliciesInput{RoleName: aws.String(role)}
	pager := func(out *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
		for _, pol := range out.AttachedPolicies {
			arns = append(arns, aws.StringValue(pol.PolicyArn))
		}
		return true
	}
	if err := c.ListAttachedRolePoliciesPages(&in, pager); err != nil {
		return err
	}
	return goForEach(arns, func(arn interface{}) error {
		in := iam.DetachRolePolicyInput{
			PolicyArn: aws.String(arn.(string)),
			RoleName:  aws.String(role),
		}
		_, err := c.DetachRolePolicy(&in)
		return err
	})
}

// IsAWSAccountID tests whether id is a valid AWS account ID.
func IsAWSAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 11; i >= 0; i-- {
		if c := id[i]; c < '0' || '9' < c {
			return false
		}
	}
	return true
}

// goForEach takes a slice of input values and calls fn on each one in a
// separate goroutine. Only one non-nil error is returned.
func goForEach(in interface{}, fn func(v interface{}) error) error {
	inv := reflect.ValueOf(in)
	n := inv.Len()
	if n <= 1 {
		if n == 0 {
			return nil
		}
		return fn(inv.Index(0).Interface())
	}
	ch := make(chan error)
	var err error
	for i, j := 0, 0; i < n; {
		if j += 100; j > n {
			j = n
		}
		running := j - i
		for ; i < j; i++ {
			go func(v interface{}) {
				ch <- fn(v)
			}(inv.Index(i).Interface())
		}
		for e := range ch {
			if e != nil {
				err = e
			}
			if running--; running == 0 {
				break
			}
		}
	}
	return err
}
