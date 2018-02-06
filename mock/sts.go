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

// STSRouter handles STS API calls. Key is SessionToken, which is the ARN of the
// assumed role.
type STSRouter map[string]*sts.GetCallerIdentityOutput

// NewSTSRouter returns a router configured to handle permanent credentials.
func NewSTSRouter() STSRouter {
	return map[string]*sts.GetCallerIdentityOutput{"": {
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:sts::000000000000:assumed-role/TestRole/TestSession"),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:user@example.com"),
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
	token := aws.StringValue(in.RoleArn)
	role, err := arn.Parse(token)
	if err != nil {
		panic(err)
	}
	i := strings.LastIndexByte(token, '/')
	name := token[i+1:]
	sess := aws.StringValue(in.RoleSessionName)
	sessArn := fmt.Sprintf("arn:aws:sts::%s:assumed-role/%s/%s", role.AccountID, name, sess)
	r[token] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.AccountID),
		Arn:     aws.String(sessArn),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:" + sess),
	}
	out := q.Data.(*sts.AssumeRoleOutput)
	out.Credentials = &sts.Credentials{
		AccessKeyId:     aws.String("AccessKeyId"),
		Expiration:      aws.Time(internal.Time().Add(time.Hour)),
		SecretAccessKey: aws.String("SecretAccessKey"),
		SessionToken:    aws.String(token),
	}
}

func (r STSRouter) getCallerIdentity(q *request.Request) {
	v, err := q.Config.Credentials.Get()
	if err != nil {
		q.Error = err
		return
	}
	out := r[v.SessionToken]
	if out == nil {
		panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
	}
	*q.Data.(*sts.GetCallerIdentityOutput) = *out
}
