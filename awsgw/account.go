package awsgw

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
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

// set updates account information.
func (ac *Account) set(src *orgs.Account) {
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
	Account
	CredsProvider
	creds *credentials.Credentials
}

// accountState contains serialized accountCtx state.
type accountState struct {
	Account *Account
	Creds   *StaticCreds
}

// newAccountCtx creates a new context for the specified account id.
func newAccountCtx(c *Client, id string, s accountState) *accountCtx {
	role := "arn:aws:iam::" + id + ":role/" + c.CommonRole
	ac := &accountCtx{
		Account:       Account{ID: id},
		CredsProvider: NewAssumeRoleCreds(c.sts.AssumeRole, role, c.roleSessionName),
	}
	if s.Account != nil {
		ac.Account = *s.Account
		ac.CredsProvider = NewChainCreds(c.minExp, s.Creds, ac.CredsProvider)
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
