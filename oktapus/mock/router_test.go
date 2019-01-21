package mock

import (
	"testing"

	"github.com/mxk/cloudcover/x/arn"
	"github.com/stretchr/testify/assert"
)

func TestCtxRouter(t *testing.T) {
	r := CtxRouter{}
	assert.False(t, r.Route(new(Request)))
	assert.Nil(t, r.match(Ctx))
	assert.NotNil(t, r.Root())

	want := ChainRouter{r.Root()}
	assert.Equal(t, want, r.match(Ctx))

	gov := r.Get(arn.Ctx{Partition: "aws-us-gov"})
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(arn.Ctx{Partition: Ctx.Partition}))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(arn.Ctx{Region: Ctx.Region}))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Account(Ctx.Account))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(arn.Ctx{Partition: Ctx.Partition, Region: Ctx.Region}))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(arn.Ctx{Partition: Ctx.Partition, Account: Ctx.Account}))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(arn.Ctx{Region: Ctx.Region, Account: Ctx.Account}))
	assert.Equal(t, want, r.match(Ctx))

	want = append(want, r.Get(Ctx))
	assert.Equal(t, want, r.match(Ctx))

	ctx := arn.Ctx{Partition: "aws-us-gov", Region: "us-gov-west-1", Account: "100000000000"}
	want = ChainRouter{r.Root(), gov}
	assert.Equal(t, want, r.match(ctx))

	var called bool
	r.Root().Add(RouterFunc(func(q *Request) bool {
		called = true
		return true
	}))
	assert.True(t, r.Route(&Request{Ctx: ctx}))
	assert.True(t, called)
}

// TODO: Test others
