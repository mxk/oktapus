package mock

import (
	"sync"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/awsmock"
	"github.com/LuminalHQ/cloudcover/x/region"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
)

// Mock credentials.
const (
	AccessKeyID     = "AKIAIOSFODNN7EXAMPLE"
	SecretAccessKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

// AccountID validates account id and pads it with leading zeros if required.
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
	ChainRouter
	Cfg aws.Config
	mu  sync.Mutex
}

// NewAWS returns a mock AWS cloud configured with the specified region, which
// determines the partition, and routers. STS and account routers are used by
// default if no others are specified.
func NewAWS(region string, r ...Router) *AWS {
	if region == "" {
		region = endpoints.UsEast1RegionID
	}
	w := &AWS{ChainRouter: r}
	w.Cfg = awsmock.Config(func(q *aws.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.Route(&Request{q, ctx(&q.Config)}) {
			api := q.Metadata.ServiceName + ":" + q.Operation.Name
			panic("mock: " + api + " not handled")
		}
	})
	w.Cfg.Region = region
	w.Cfg.Credentials = aws.NewStaticCredentialsProvider(
		AccessKeyID, SecretAccessKey, "")
	if r == nil {
		w.ChainRouter = ChainRouter{STSRouter{}, AccountRouter{}}
		w.AccountRouter().Add(NewAccount(w.Ctx(), "0", "master"))
	}
	return w
}

// Ctx returns the ARN context for the active client configuration.
func (w *AWS) Ctx() arn.Ctx { return ctx(&w.Cfg) }

// ctx extracts ARN context from client config.
func ctx(c *aws.Config) arn.Ctx {
	cr, err := c.Credentials.Retrieve()
	if err != nil {
		panic(err)
	}
	ac := "000000000000"
	if cr.SessionToken != "" {
		ac = arn.ARN(cr.SessionToken).Account()
	}
	return arn.Ctx{
		Partition: region.Partition(c.Region),
		Region:    c.Region,
		Account:   ac,
	}
}
