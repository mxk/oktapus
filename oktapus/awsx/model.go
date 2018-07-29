package awsx

import (
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
)

// Org contains data from organizations:DescribeOrganization API call.
type Org struct {
	ARN         arn.ARN
	FeatureSet  orgs.OrganizationFeatureSet
	ID          string
	MasterARN   arn.ARN
	MasterEmail string
	MasterID    string
}

// Set updates organization information.
func (o *Org) Set(out *orgs.DescribeOrganizationOutput) {
	*o = Org{
		ARN:         arn.Value(out.Organization.Arn),
		FeatureSet:  out.Organization.FeatureSet,
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
	Status     orgs.AccountStatus
	JoinMethod orgs.AccountJoinedMethod
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
	ac.Status = src.Status
	ac.JoinMethod = src.JoinedMethod
	ac.JoinTime = aws.TimeValue(src.JoinedTimestamp)
}
