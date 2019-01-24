package op

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/mxk/go-cloud/aws/awsmock"
	"github.com/mxk/go-cloud/aws/iamx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtl(t *testing.T) {
	c := newCtlIAM()
	var set, get Ctl

	require.NoError(t, get.Load(c.iam))
	assert.Equal(t, set, get)
	c.desc = aws.String("")
	require.NoError(t, get.Load(c.iam))
	assert.Equal(t, set, get)

	set = Ctl{Owner: "alice", Desc: "desc", Tags: Tags{"init"}}
	require.NoError(t, set.Init(c.iam))
	require.NoError(t, get.Load(c.iam))
	assert.Equal(t, set, get)

	set = Ctl{Tags: Tags{"tag"}}
	require.NoError(t, set.Store(c.iam))
	require.NoError(t, get.Load(c.iam))
	assert.Equal(t, set, get)

	c.err = awserr.New(iam.ErrCodeNoSuchEntityException, "", nil)
	require.Error(t, ErrNoCtl, get.Load(c.iam))
	require.Error(t, ErrNoCtl, set.Store(c.iam))
	assert.Equal(t, Ctl{}, get)

	set = Ctl{Owner: "bob"}
	get = set
	c.err = ErrCtlUpdate
	require.EqualError(t, set.Store(c.iam), ErrCtlUpdate.Error())
	assert.Equal(t, set, get)

	c.err = nil
	c.desc = aws.String(ctlVer)
	assert.Error(t, get.Load(c.iam))
	assert.Equal(t, Ctl{}, get)

	get = set
	c.desc = aws.String("abc=")
	assert.Error(t, get.Load(c.iam))
	assert.Equal(t, Ctl{}, get)
}

func TestCtlEq(t *testing.T) {
	tests := []*struct {
		a, b Ctl
		eq   bool
	}{
		{Ctl{}, Ctl{}, true},
		{Ctl{Owner: "a"}, Ctl{}, false},
		{Ctl{Owner: "a"}, Ctl{Owner: "a"}, true},
		{Ctl{Desc: "b"}, Ctl{}, false},
		{Ctl{Desc: "b"}, Ctl{Desc: "b"}, true},
		{Ctl{Tags: Tags{"c"}}, Ctl{}, false},
		{Ctl{Tags: Tags{"c"}}, Ctl{Tags: Tags{"c"}}, true},
		{Ctl{Tags: Tags{"c", "d"}}, Ctl{Tags: Tags{"c"}}, false},
		{Ctl{Tags: Tags{"c", "d"}}, Ctl{Tags: Tags{"d", "c"}}, false},
		{Ctl{Tags: Tags{"c", "d"}}, Ctl{Tags: Tags{"c", "d"}}, true},
	}
	for _, test := range tests {
		assert.Equal(t, test.eq, test.a.eq(&test.b), "a=%#v b=%#v", test.a, test.b)
		assert.Equal(t, test.eq, test.b.eq(&test.a), "a=%#v b=%#v", test.a, test.b)
	}
}

func TestCtlMerge(t *testing.T) {
	tests := []*struct {
		ctl, cur, ref, want Ctl
	}{{
		ctl:  Ctl{},
		cur:  Ctl{},
		ref:  Ctl{},
		want: Ctl{},
	}, {
		ctl:  Ctl{Owner: "a"},
		cur:  Ctl{},
		ref:  Ctl{},
		want: Ctl{Owner: "a"},
	}, {
		ctl:  Ctl{Owner: "b"},
		cur:  Ctl{Owner: "c"},
		ref:  Ctl{},
		want: Ctl{Owner: "b"},
	}, {
		ctl:  Ctl{Owner: "c"},
		cur:  Ctl{Owner: "d"},
		ref:  Ctl{Owner: "c"},
		want: Ctl{Owner: "d"},
	}, {
		ctl:  Ctl{Desc: "a"},
		cur:  Ctl{},
		ref:  Ctl{Desc: "x"},
		want: Ctl{Desc: "a"},
	}, {
		ctl:  Ctl{Desc: "b"},
		cur:  Ctl{Desc: "c"},
		ref:  Ctl{Desc: "x"},
		want: Ctl{Desc: "b"},
	}, {
		ctl:  Ctl{Desc: "c"},
		cur:  Ctl{Desc: "d"},
		ref:  Ctl{Desc: "c"},
		want: Ctl{Desc: "d"},
	}, {
		ctl:  Ctl{Tags: Tags{"a"}},
		cur:  Ctl{Tags: Tags{"b", "c"}},
		ref:  Ctl{Tags: Tags{"b"}},
		want: Ctl{Tags: Tags{"a", "c"}},
	}}
	for _, test := range tests {
		test.ctl.merge(&test.cur, &test.ref)
		assert.Equal(t, test.want, test.ctl)
	}
}

func TestCtlAlias(t *testing.T) {
	ctl := Ctl{Tags: Tags{"a", "b", "c"}}
	cur := Ctl{}
	ref := ctl
	assert.Panics(t, func() { ctl.copy(&ref) })
	assert.Panics(t, func() { ctl.merge(&cur, &ref) })
	assert.Panics(t, func() { ctl.merge(&ref, &cur) })
	assert.Panics(t, func() { cur.merge(&ctl, &ref) })
}

type ctlIAM struct {
	iam  iamx.Client
	desc *string
	err  error
}

func newCtlIAM() *ctlIAM {
	c := new(ctlIAM)
	cfg := awsmock.Config(func(q *aws.Request) {
		switch in := q.Params.(type) {
		case *iam.CreateRoleInput:
			q.Data, q.Error = c.createRole(in)
		case *iam.GetRoleInput:
			q.Data, q.Error = c.getRole(in)
		case *iam.UpdateRoleDescriptionInput:
			q.Data, q.Error = c.updateRoleDescription(in)
		default:
			panic("unsupported api: " + q.Operation.Name)
		}
	})
	c.iam = iamx.New(&cfg)
	return c
}

func (c *ctlIAM) createRole(in *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	if c.err != nil {
		return new(iam.CreateRoleOutput), c.err
	}
	c.desc = in.Description
	return &iam.CreateRoleOutput{Role: &iam.Role{}}, nil
}

func (c *ctlIAM) getRole(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	if c.err != nil {
		return new(iam.GetRoleOutput), c.err
	}
	return &iam.GetRoleOutput{Role: &iam.Role{
		Description: c.desc,
	}}, nil
}

func (c *ctlIAM) updateRoleDescription(in *iam.UpdateRoleDescriptionInput) (*iam.UpdateRoleDescriptionOutput, error) {
	if c.err == nil {
		c.desc = in.Description
	} else if c.err != ErrCtlUpdate {
		return new(iam.UpdateRoleDescriptionOutput), c.err
	}
	return &iam.UpdateRoleDescriptionOutput{Role: &iam.Role{
		Description: c.desc,
	}}, nil
}
