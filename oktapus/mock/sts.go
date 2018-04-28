package mock

import (
	"fmt"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/sts"
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
func (r STSRouter) Route(_ *Session, q *request.Request, api string) bool {
	switch api {
	case "sts:AssumeRole":
		r.assumeRole(q)
	case "sts:GetCallerIdentity":
		r.getCallerIdentity(q)
	default:
		return false
	}
	return true
}

func (r STSRouter) assumeRole(q *request.Request) {
	in := q.Params.(*sts.AssumeRoleInput)
	role, err := arn.Parse(aws.StringValue(in.RoleArn))
	if err != nil {
		panic(err)
	}
	i := strings.LastIndexByte(role.Resource, '/')
	roleSessName := aws.StringValue(in.RoleSessionName)
	sessArn := AssumedRoleARN(role.AccountID, role.Resource[i+1:], roleSessName)
	r[sessArn] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.AccountID),
		Arn:     aws.String(sessArn),
		UserId:  aws.String(roleID + ":" + roleSessName),
	}
	q.Data.(*sts.AssumeRoleOutput).Credentials = &sts.Credentials{
		AccessKeyId:     aws.String(AccessKeyID),
		Expiration:      aws.Time(internal.Time().Add(time.Hour)),
		SecretAccessKey: aws.String(SecretAccessKey),
		SessionToken:    aws.String(sessArn),
	}
}

func (r STSRouter) getCallerIdentity(q *request.Request) {
	v, err := q.Config.Credentials.Get()
	if err != nil {
		panic(err)
	}
	if out := r[v.SessionToken]; out != nil {
		*q.Data.(*sts.GetCallerIdentityOutput) = *out
		return
	}
	panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
}
