package mock

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/mxk/cloudcover/x/arn"
	"github.com/mxk/cloudcover/x/fast"
)

// STSRouter handles STS API calls. Key is SessionToken, which is the assumed
// role ARN, as returned by GetCallerIdentity.
type STSRouter map[arn.ARN]*sts.GetCallerIdentityOutput

// Route implements the Router interface.
func (r STSRouter) Route(q *Request) bool { return RouteMethod(r, q) }

func (r STSRouter) AssumeRole(q *Request, in *sts.AssumeRoleInput) {
	role := arn.Value(in.RoleArn)
	sessName := aws.StringValue(in.RoleSessionName)
	ctx := q.Ctx
	ctx.Account = role.Account()
	sess := ctx.New("sts", "assumed-role/", role.Name(), "/", sessName)
	r[sess] = &sts.GetCallerIdentityOutput{
		Account: aws.String(ctx.Account),
		Arn:     arn.String(sess),
		UserId:  aws.String("AROACKCEVSQ6C2EXAMPLE:" + sessName),
	}
	q.Data.(*sts.AssumeRoleOutput).Credentials = &sts.Credentials{
		AccessKeyId:     aws.String("ASIAIOSFODNN7EXAMPLE"),
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
	sess := arn.ARN(v.SessionToken)
	out := r[sess]
	if out == nil {
		if sess == "" {
			sess = q.Ctx.New("iam", "user/alice")
		} else if sess.Service() != "iam" || sess.Type() != "user" {
			panic(fmt.Sprintf("mock: invalid session token %q", string(sess)))
		}
		out = &sts.GetCallerIdentityOutput{
			Account: aws.String(q.Ctx.Account),
			Arn:     arn.String(sess),
			UserId:  aws.String("AIDACKCEVSQ6C2EXAMPLE"),
		}
	}
	*q.Data.(*sts.GetCallerIdentityOutput) = *out
}
