package awsx

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
)

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
	if id := aws.StringValue(src.Id); id == "" || ac.ID != id {
		panic("awsx: account id mismatch: " + ac.ID + " != " + id)
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
	cp CredsProvider
}

// accountState contains saved accountCtx state.
type accountState struct {
	Account *Account
	Creds   *StaticCreds
}

// restore creates an accountCtx from the saved state.
func (s *accountState) restore(cp CredsProvider) *accountCtx {
	return &accountCtx{*s.Account, NewSavedCreds(s.Creds, cp)}
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
