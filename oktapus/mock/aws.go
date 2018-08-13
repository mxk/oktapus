package mock

import (
	"fmt"
	"path"
	"sync"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/awsmock"
	"github.com/LuminalHQ/cloudcover/x/region"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// Mock STS credentials.
const (
	AccessKeyID     = "ASIAIOSFODNN7EXAMPLE"
	SecretAccessKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY"
)

// Ctx is a mock ARN context.
var Ctx = arn.Ctx{Partition: "aws", Region: "us-east-1", Account: "000000000000"}

// AccountID validates account id and pads it with leading zeros, if required.
func AccountID(id string) string {
	for i, c := range []byte(id) {
		if c-'0' > 9 || i == 12 {
			panic("mock: invalid account id: " + id)
		}
	}
	if len(id) < 12 {
		b := []byte("000000000000")
		copy(b[12-len(id):], id)
		id = string(b)
	}
	return id
}

// AWS is a mock AWS cloud that uses routers to handle requests.
type AWS struct {
	CtxRouter
	Ctx arn.Ctx
	Cfg aws.Config
	mu  sync.Mutex
}

// NewAWS returns a mock AWS cloud configured with the specified context and
// custom routers. The STS router is added to the root context automatically.
func NewAWS(ctx arn.Ctx, r ...Router) *AWS {
	root := append(ChainRouter{STSRouter{}}, r...)
	w := &AWS{
		CtxRouter: CtxRouter{arn.Ctx{}: &root},
		Ctx:       ctx,
	}
	w.Cfg = awsmock.Config(func(q *aws.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if q := w.newRequest(q); !w.Route(q) {
			panic("mock: " + q.Name() + " not handled")
		}
	})
	w.Cfg.Region = ctx.Region
	w.Cfg.Credentials = w.UserCreds("", "alice")
	return w
}

// UserCreds returns a mock credentials provider for the specified account/user.
func (w *AWS) UserCreds(account, user string) aws.StaticCredentialsProvider {
	ctx := w.Ctx
	if account != "" {
		ctx.Account = AccountID(account)
	}
	return aws.NewStaticCredentialsProvider(
		"AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey,
		string(ctx.New("iam", "user", path.Clean("/"+user))),
	)
}

// newRequest creates a new mock request and performs basic validations.
func (w *AWS) newRequest(r *aws.Request) *Request {
	ctx := arn.Ctx{
		Partition: region.Partition(r.Config.Region),
		Region:    r.Config.Region,
		Account:   w.Ctx.Account,
	}
	cr, err := r.Config.Credentials.Retrieve()
	if err != nil {
		panic(err)
	}
	if cr.SessionToken != "" {
		ctx.Account = arn.ARN(cr.SessionToken).Account()
	}
	q := &Request{r, w, ctx}
	if q.Ctx.Partition != w.Ctx.Partition {
		panic(fmt.Sprintf("mock: %s called in partition %q (should be %q)",
			q.Name(), ctx.Partition, w.Ctx.Partition))
	}
	if region.Subset(ctx.Partition, q.Metadata.ServiceName) == nil {
		panic(fmt.Sprintf("mock: %q partition does not support %q api",
			ctx.Partition, q.Metadata.ServiceName))
	}
	return q
}

// Request wraps aws.Request to provide additional information.
type Request struct {
	*aws.Request
	AWS *AWS
	Ctx arn.Ctx
}

// Name returns the service-qualified API name.
func (q *Request) Name() string {
	return q.Metadata.ServiceName + ":" + q.Operation.Name
}
