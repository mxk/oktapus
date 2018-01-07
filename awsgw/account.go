package awsgw

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// TODO: Figure out which account fields are mutable

// Account is an account in an AWS organization.
type Account struct {
	ID         string
	ARN        string
	Name       string
	Email      string
	Status     string
	JoinMethod string
	JoinTime   time.Time
}

// update updates account fields from src.
func (ac *Account) update(src *orgs.Account) {
	if id := aws.StringValue(src.Id); ac.ID != id {
		panic("awsgw: account id mismatch: " + ac.ID + " != " + id)
	}
	ac.ARN = aws.StringValue(src.Arn)
	ac.Name = aws.StringValue(src.Name)
	ac.Email = aws.StringValue(src.Email)
	ac.Status = accountStatusEnum(src.Status)
	ac.JoinMethod = joinedMethodEnum(src.JoinedMethod)
	ac.JoinTime = aws.TimeValue(src.JoinedTimestamp)
}

// accountCtx maintains runtime context for each account.
type accountCtx struct {
	AssumeRoleCredsProvider

	info  Account
	creds *credentials.Credentials
}

// newAccountCtx creates a new context for the specified account id.
func newAccountCtx(c *Client, id string) *accountCtx {
	role := "arn:aws:iam::" + id + ":role/" + c.CommonRole
	ac := &accountCtx{
		AssumeRoleCredsProvider: AssumeRoleCredsProvider{
			AssumeRoleInput: sts.AssumeRoleInput{
				RoleArn:         aws.String(role),
				RoleSessionName: aws.String(c.roleSessionName),
			},
			API: c.sts.AssumeRole,
		},
		info: Account{ID: id},
	}
	ac.creds = credentials.NewCredentials(ac)
	return ac
}

// accountStatusEnum returns AccountStatus enum string without allocation.
func accountStatusEnum(s *string) string {
	if s == nil {
		return ""
	}
	switch *s {
	case orgs.AccountStatusActive:
		return orgs.AccountStatusActive
	case orgs.AccountStatusSuspended:
		return orgs.AccountStatusSuspended
	}
	return *s
}

// joinedMethodEnum returns AccountJoinedMethod enum string without allocation.
func joinedMethodEnum(s *string) string {
	if s == nil {
		return ""
	}
	switch *s {
	case orgs.AccountJoinedMethodInvited:
		return orgs.AccountJoinedMethodInvited
	case orgs.AccountJoinedMethodCreated:
		return orgs.AccountJoinedMethodCreated
	}
	return *s
}
