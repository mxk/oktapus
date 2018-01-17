package cmd

import (
	"fmt"
	"reflect"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

const assumeRolePolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "%s",
		"Principal": {"AWS": "%s"},
		"Action": "sts:AssumeRole"
	}]
}`

const adminPolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Action": "*",
		"Resource": "*"
	}]
}`

// tmpIAMPath is a path for temporary users and roles.
const tmpIAMPath = ctlPath + "tmp/"

// newAssumeRolePolicy returns an AssumeRole policy document that is used when
// creating new roles.
func newAssumeRolePolicy(principal string) string {
	if principal == "" {
		return fmt.Sprintf(assumeRolePolicy, "Deny", "*")
	}
	if isAWSAccountID(principal) {
		return fmt.Sprintf(assumeRolePolicy, "Allow",
			"arn:aws:iam::"+principal+":root")
	}
	return fmt.Sprintf(assumeRolePolicy, "Allow", principal)
}

// delTmpUsers deletes all users under the temporary IAM path.
func delTmpUsers(c *iam.IAM) error {
	var users []string
	in := iam.ListUsersInput{PathPrefix: aws.String(tmpIAMPath)}
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
func delUser(c *iam.IAM, user string) error {
	var pols []string
	in := iam.ListAttachedUserPoliciesInput{UserName: aws.String(user)}
	pager := func(out *iam.ListAttachedUserPoliciesOutput, lastPage bool) bool {
		for _, pol := range out.AttachedPolicies {
			pols = append(pols, aws.StringValue(pol.PolicyArn))
		}
		return true
	}
	if err := c.ListAttachedUserPoliciesPages(&in, pager); err != nil {
		return err
	}
	err := goForEach(pols, func(pol interface{}) error {
		in := iam.DetachUserPolicyInput{
			PolicyArn: aws.String(pol.(string)),
			UserName:  aws.String(user),
		}
		_, err := c.DetachUserPolicy(&in)
		return err
	})
	if err != nil {
		return err
	}
	in2 := iam.DeleteUserInput{UserName: aws.String(user)}
	_, err = c.DeleteUser(&in2)
	return err
}

// delTmpRoles deletes all roles under the temporary IAM path.
func delTmpRoles(c *iam.IAM) error {
	var roles []string
	in := iam.ListRolesInput{PathPrefix: aws.String(tmpIAMPath)}
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
func delRole(c *iam.IAM, role string) error {
	var pols []string
	in := iam.ListAttachedRolePoliciesInput{RoleName: aws.String(role)}
	pager := func(out *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
		for _, pol := range out.AttachedPolicies {
			pols = append(pols, aws.StringValue(pol.PolicyArn))
		}
		return true
	}
	if err := c.ListAttachedRolePoliciesPages(&in, pager); err != nil {
		return err
	}
	err := goForEach(pols, func(pol interface{}) error {
		in := iam.DetachRolePolicyInput{
			PolicyArn: aws.String(pol.(string)),
			RoleName:  aws.String(role),
		}
		_, err := c.DetachRolePolicy(&in)
		return err
	})
	if err != nil {
		return err
	}
	in2 := iam.DeleteRoleInput{RoleName: aws.String(role)}
	_, err = c.DeleteRole(&in2)
	return err
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
