package mock

import (
	"fmt"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// STSRouter handles STS API calls. Key is SessionToken, which is the assumed
// role ARN, as returned by GetCallerIdentity.
type STSRouter map[arn.ARN]*sts.GetCallerIdentityOutput

// Route implements the Router interface.
func (r STSRouter) Route(q *Request) bool { return RouteMethod(r, q) }

func (r STSRouter) AssumeRole(q *Request, in *sts.AssumeRoleInput) {
	role := arn.Value(in.RoleArn)
	sessName := aws.StringValue(in.RoleSessionName)
	sess := q.Ctx.New("sts", "assumed-role/", role.Name(), "/", sessName)
	r[sess] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.Account()),
		Arn:     arn.String(sess),
		UserId:  aws.String("AROA:" + sessName),
	}
	q.Data.(*sts.AssumeRoleOutput).Credentials = &sts.Credentials{
		AccessKeyId:     aws.String(AccessKeyID),
		Expiration:      aws.Time(fast.Time().Add(time.Hour)),
		SecretAccessKey: aws.String(SecretAccessKey),
		SessionToken:    arn.String(sess),
	}
}

func (r STSRouter) GetCallerIdentity(q *Request, _ *sts.GetCallerIdentityInput) {
	v, err := q.Config.Credentials.Retrieve()
	if err != nil {
		panic(err)
	}
	out := r[arn.ARN(v.SessionToken)]
	if out == nil {
		if v.SessionToken != "" {
			panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
		}
		sess := q.Ctx.New("iam", "user/user@example.com")
		out = &sts.GetCallerIdentityOutput{
			Account: aws.String(q.Ctx.Account),
			Arn:     arn.String(sess),
			UserId:  aws.String("AIDA"),
		}
	}
	*q.Data.(*sts.GetCallerIdentityOutput) = *out
}
