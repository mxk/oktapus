package mock

import (
	"fmt"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	roleID       = "AKIAI44QH8DHBEXAMPLE"
	roleSessName = "user@example.com"
)

// STSRouter handles STS API calls. Key is SessionToken, which is the assumed
// role ARN, as returned by GetCallerIdentity.
type STSRouter map[string]*sts.GetCallerIdentityOutput

// NewSTSRouter returns a router configured to handle permanent credentials.
func NewSTSRouter(gwAccountID string) STSRouter {
	gwAccountID = AccountID(gwAccountID)
	sessArn := AssumedRoleARN(gwAccountID, "GatewayRole", roleSessName)
	return map[string]*sts.GetCallerIdentityOutput{"": {
		Account: aws.String(gwAccountID),
		Arn:     aws.String(sessArn),
		UserId:  aws.String(roleID + ":" + roleSessName),
	}}
}

// Route implements the Router interface.
func (r STSRouter) Route(q *aws.Request) bool {
	switch q.Params.(type) {
	case *sts.AssumeRoleInput:
		r.assumeRole(q)
	case *sts.GetCallerIdentityInput:
		r.getCallerIdentity(q)
	default:
		return false
	}
	return true
}

func (r STSRouter) assumeRole(q *aws.Request) {
	in := q.Params.(*sts.AssumeRoleInput)
	role := arn.Value(in.RoleArn)
	roleSessName := aws.StringValue(in.RoleSessionName)
	sessArn := AssumedRoleARN(role.Account(), role.Name(), roleSessName)
	r[sessArn] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.Account()),
		Arn:     aws.String(sessArn),
		UserId:  aws.String(roleID + ":" + roleSessName),
	}
	q.Data.(*sts.AssumeRoleOutput).Credentials = &sts.Credentials{
		AccessKeyId:     aws.String(AccessKeyID),
		Expiration:      aws.Time(fast.Time().Add(time.Hour)),
		SecretAccessKey: aws.String(SecretAccessKey),
		SessionToken:    aws.String(sessArn),
	}
}

func (r STSRouter) getCallerIdentity(q *aws.Request) {
	v, err := q.Config.Credentials.Retrieve()
	if err != nil {
		panic(err)
	}
	if out := r[v.SessionToken]; out != nil {
		*q.Data.(*sts.GetCallerIdentityOutput) = *out
		return
	}
	panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
}
