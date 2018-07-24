package awsx

import (
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go/aws"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Ident contains data from sts:GetCallerIdentity API call.
type Ident struct {
	AccountID string
	UserARN   arn.ARN
	UserID    string
}

// Set updates identity information.
func (id *Ident) Set(out *sts.GetCallerIdentityOutput) {
	*id = Ident{
		AccountID: aws.StringValue(out.Account),
		UserARN:   arn.Value(out.Arn),
		UserID:    aws.StringValue(out.UserId),
	}
}

// Org contains data from organizations:DescribeOrganization API call.
type Org struct {
	ARN         arn.ARN
	FeatureSet  string
	ID          string
	MasterARN   arn.ARN
	MasterEmail string
	MasterID    string
}

// Set updates organization information.
func (o *Org) Set(out *orgs.DescribeOrganizationOutput) {
	*o = Org{
		ARN:         arn.Value(out.Organization.Arn),
		FeatureSet:  aws.StringValue(out.Organization.FeatureSet),
		ID:          aws.StringValue(out.Organization.Id),
		MasterARN:   arn.Value(out.Organization.MasterAccountArn),
		MasterEmail: aws.StringValue(out.Organization.MasterAccountEmail),
		MasterID:    aws.StringValue(out.Organization.MasterAccountId),
	}
}

// Account is an account in an AWS organization.
type Account struct {
	ID         string
	ARN        arn.ARN
	Name       string
	Email      string
	Status     string
	JoinMethod string
	JoinTime   time.Time
}

// Set updates account information.
func (ac *Account) Set(src *orgs.Account) {
	if id := aws.StringValue(src.Id); id == "" || ac.ID != id {
		panic("awsx: account id mismatch: " + ac.ID + " != " + id)
	}
	ac.ARN = arn.Value(src.Arn)
	ac.Name = aws.StringValue(src.Name)
	ac.Email = aws.StringValue(src.Email)
	ac.Status = accountStatusEnum(src.Status)
	ac.JoinMethod = joinedMethodEnum(src.JoinedMethod)
	ac.JoinTime = aws.TimeValue(src.JoinedTimestamp)
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
