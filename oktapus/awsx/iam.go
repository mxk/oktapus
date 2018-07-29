package awsx

import (
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/iamiface"
)

// DeleteUsers deletes all users under the specified IAM path.
func DeleteUsers(c iamiface.IAMAPI, path string) error {
	in := iam.ListUsersInput{PathPrefix: aws.String(path)}
	r := c.ListUsersRequest(&in)
	p := r.Paginate()
	var users []string
	for p.Next() {
		for _, u := range p.CurrentPage().Users {
			users = append(users, aws.StringValue(u.UserName))
		}
	}
	if err := p.Err(); err != nil {
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
		_, err = c.DeleteUserRequest(&in).Send()
	}
	return err
}

// deleteAccessKeys deletes all user access keys.
func deleteAccessKeys(c iamiface.IAMAPI, user string) error {
	in := iam.ListAccessKeysInput{UserName: aws.String(user)}
	r := c.ListAccessKeysRequest(&in)
	p := r.Paginate()
	var ids []string
	for p.Next() {
		for _, key := range p.CurrentPage().AccessKeyMetadata {
			ids = append(ids, aws.StringValue(key.AccessKeyId))
		}
	}
	if err := p.Err(); err != nil {
		return err
	}
	return fast.ForEachIO(len(ids), func(i int) error {
		in := iam.DeleteAccessKeyInput{
			AccessKeyId: aws.String(ids[i]),
			UserName:    aws.String(user),
		}
		_, err := c.DeleteAccessKeyRequest(&in).Send()
		return err
	})
}

// detachUserPolicies detaches all user policies.
func detachUserPolicies(c iamiface.IAMAPI, user string) error {
	in := iam.ListAttachedUserPoliciesInput{UserName: aws.String(user)}
	r := c.ListAttachedUserPoliciesRequest(&in)
	p := r.Paginate()
	var arns []string
	for p.Next() {
		for _, pol := range p.CurrentPage().AttachedPolicies {
			arns = append(arns, aws.StringValue(pol.PolicyArn))
		}
	}
	if err := p.Err(); err != nil {
		return err
	}
	return fast.ForEachIO(len(arns), func(i int) error {
		in := iam.DetachUserPolicyInput{
			PolicyArn: aws.String(arns[i]),
			UserName:  aws.String(user),
		}
		_, err := c.DetachUserPolicyRequest(&in).Send()
		return err
	})
}

// DeleteRoles deletes all roles under the specified IAM path.
func DeleteRoles(c iamiface.IAMAPI, path string) error {
	in := iam.ListRolesInput{PathPrefix: aws.String(path)}
	r := c.ListRolesRequest(&in)
	p := r.Paginate()
	var roles []string
	for p.Next() {
		for _, r := range p.CurrentPage().Roles {
			roles = append(roles, aws.StringValue(r.RoleName))
		}
	}
	if err := p.Err(); err != nil {
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
		_, err = c.DeleteRoleRequest(&in).Send()
	}
	return err
}

// detachRolePolicies detaches all role policies.
func detachRolePolicies(c iamiface.IAMAPI, role string) error {
	in := iam.ListAttachedRolePoliciesInput{RoleName: aws.String(role)}
	r := c.ListAttachedRolePoliciesRequest(&in)
	p := r.Paginate()
	var arns []string
	for p.Next() {
		for _, pol := range p.CurrentPage().AttachedPolicies {
			arns = append(arns, aws.StringValue(pol.PolicyArn))
		}
	}
	if err := p.Err(); err != nil {
		return err
	}
	return fast.ForEachIO(len(arns), func(i int) error {
		in := iam.DetachRolePolicyInput{
			PolicyArn: aws.String(arns[i]),
			RoleName:  aws.String(role),
		}
		_, err := c.DetachRolePolicyRequest(&in).Send()
		return err
	})
}

// deleteRolePolicies deletes all inline role policies.
func deleteRolePolicies(c iamiface.IAMAPI, role string) error {
	in := iam.ListRolePoliciesInput{RoleName: aws.String(role)}
	r := c.ListRolePoliciesRequest(&in)
	p := r.Paginate()
	var names []string
	for p.Next() {
		names = append(names, p.CurrentPage().PolicyNames...)
	}
	if err := p.Err(); err != nil {
		return err
	}
	return fast.ForEachIO(len(names), func(i int) error {
		in := iam.DeleteRolePolicyInput{
			PolicyName: aws.String(names[i]),
			RoleName:   aws.String(role),
		}
		_, err := c.DeleteRolePolicyRequest(&in).Send()
		return err
	})
}
