package awsx

import (
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

// DeleteUsers deletes all users under the specified IAM path.
func DeleteUsers(c iamiface.IAMAPI, path string) error {
	var users []string
	in := iam.ListUsersInput{PathPrefix: aws.String(path)}
	pager := func(out *iam.ListUsersOutput, lastPage bool) bool {
		for _, u := range out.Users {
			users = append(users, aws.StringValue(u.UserName))
		}
		return true
	}
	if err := c.ListUsersPages(&in, pager); err != nil {
		return err
	}
	return fast.ForEachIO(len(users), func(i int) error {
		return DeleteUser(c, users[i])
	})
}

// DeleteUser deletes the specified user, ensuring that all prerequisites for
// deletion are met.
func DeleteUser(c iamiface.IAMAPI, name string) error {
	err := fast.Call(
		func() error { return detachUserPolicies(c, name) },
		func() error { return deleteAccessKeys(c, name) },
	)
	if err == nil {
		in := iam.DeleteUserInput{UserName: aws.String(name)}
		_, err = c.DeleteUser(&in)
	}
	return err
}

// deleteAccessKeys deletes all user access keys.
func deleteAccessKeys(c iamiface.IAMAPI, user string) error {
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
	return fast.ForEachIO(len(ids), func(i int) error {
		in := iam.DeleteAccessKeyInput{
			AccessKeyId: aws.String(ids[i]),
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
	return fast.ForEachIO(len(arns), func(i int) error {
		in := iam.DetachUserPolicyInput{
			PolicyArn: aws.String(arns[i]),
			UserName:  aws.String(user),
		}
		_, err := c.DetachUserPolicy(&in)
		return err
	})
}

// DeleteRoles deletes all roles under the specified IAM path.
func DeleteRoles(c iamiface.IAMAPI, path string) error {
	var roles []string
	in := iam.ListRolesInput{PathPrefix: aws.String(path)}
	pager := func(out *iam.ListRolesOutput, lastPage bool) bool {
		for _, r := range out.Roles {
			roles = append(roles, aws.StringValue(r.RoleName))
		}
		return true
	}
	if err := c.ListRolesPages(&in, pager); err != nil {
		return err
	}
	return fast.ForEachIO(len(roles), func(i int) error {
		return DeleteRole(c, roles[i])
	})
}

// DeleteRole deletes the specified role, ensuring that all prerequisites for
// deletion are met.
func DeleteRole(c iamiface.IAMAPI, role string) error {
	err := fast.Call(
		func() error { return detachRolePolicies(c, role) },
		func() error { return deleteRolePolicies(c, role) },
	)
	if err == nil {
		in := iam.DeleteRoleInput{RoleName: aws.String(role)}
		_, err = c.DeleteRole(&in)
	}
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
	return fast.ForEachIO(len(arns), func(i int) error {
		in := iam.DetachRolePolicyInput{
			PolicyArn: aws.String(arns[i]),
			RoleName:  aws.String(role),
		}
		_, err := c.DetachRolePolicy(&in)
		return err
	})
}

// deleteRolePolicies deletes all inline role policies.
func deleteRolePolicies(c iamiface.IAMAPI, role string) error {
	var names []string
	in := iam.ListRolePoliciesInput{RoleName: aws.String(role)}
	pager := func(out *iam.ListRolePoliciesOutput, lastPage bool) bool {
		for _, name := range out.PolicyNames {
			names = append(names, aws.StringValue(name))
		}
		return true
	}
	if err := c.ListRolePoliciesPages(&in, pager); err != nil {
		return err
	}
	return fast.ForEachIO(len(names), func(i int) error {
		in := iam.DeleteRolePolicyInput{
			PolicyName: aws.String(names[i]),
			RoleName:   aws.String(role),
		}
		_, err := c.DeleteRolePolicy(&in)
		return err
	})
}
