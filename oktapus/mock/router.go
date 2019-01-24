package mock

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mxk/go-cloud/aws/arn"
)

// Router simulates AWS responses by populating request Data and Error fields.
// Route returns true if the request was handled or false if it should be passed
// to the next router.
type Router interface {
	Route(q *Request) bool
}

// RouteMethod uses reflection to find a method of r that matches the API name.
// If such method is found, it handles the request. The method type must be:
//
//	func(q *mock.Request, in *svc.XyzInput)
func RouteMethod(r interface{}, q *Request) bool {
	if m := reflect.ValueOf(r).MethodByName(q.Operation.Name); m.IsValid() {
		args := []reflect.Value{reflect.ValueOf(q), reflect.ValueOf(q.Params)}
		if len(m.Call(args)) != 0 {
			name := reflect.TypeOf(r).String() + "." + q.Operation.Name
			panic("mock: unexpected return value from " + name)
		}
		return true
	}
	return false
}

// RouterFunc allows using a regular function as a router.
type RouterFunc func(q *Request) bool

// Route implements the Router interface.
func (f RouterFunc) Route(q *Request) bool { return f(q) }

// CtxRouter redirects requests to other routers using the ARN context. More
// specific context keys take priority over less specific ones. The zero value
// context matches all requests.
type CtxRouter map[arn.Ctx]*ChainRouter

// Route implements the Router interface.
func (r CtxRouter) Route(q *Request) bool {
	if cr := r.match(q.Ctx); cr != nil {
		return cr.Route(q)
	}
	return false
}

// Root returns the router that matches all contexts.
func (r CtxRouter) Root() *ChainRouter {
	return r.Get(arn.Ctx{})
}

// Account returns the router for the specified account ID.
func (r CtxRouter) Account(id string) *ChainRouter {
	return r.Get(arn.Ctx{Account: AccountID(id)})
}

// Get returns the router for the specified context. A new router is created if
// one does not exist.
func (r CtxRouter) Get(ctx arn.Ctx) *ChainRouter {
	cr := r[ctx]
	if cr == nil {
		cr = new(ChainRouter)
		r[ctx] = cr
	}
	return cr
}

// match returns a new ChainRouter containing other ChainRouters that are able
// to handle the specified context.
func (r CtxRouter) match(ctx arn.Ctx) ChainRouter {
	cmp := func(spec, want string, flag int) int {
		switch spec {
		case want:
			return flag
		case "":
			return 0
		}
		return -1
	}
	var matches [8]*ChainRouter
	n := 0
	for k, v := range r {
		score := cmp(k.Partition, ctx.Partition, 1)
		score |= cmp(k.Region, ctx.Region, 2)
		score |= cmp(k.Account, ctx.Account, 4)
		if score >= 0 {
			matches[score] = v
			n++
		}
	}
	var cr ChainRouter
	if n > 0 {
		cr = make(ChainRouter, 0, n)
		for _, m := range matches {
			if m != nil {
				if cr = append(cr, m); len(cr) == cap(cr) {
					break
				}
			}
		}
	}
	return cr
}

// ChainRouter maintains a router chain, passing requests to each router,
// starting with the last one, until the request is handled.
type ChainRouter []Router

// Route implements the Router interface.
func (r ChainRouter) Route(q *Request) bool {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i].Route(q) {
			return true
		}
	}
	return false
}

// Add appends new routers to the chain, giving them highest priority.
func (r *ChainRouter) Add(t ...Router) {
	*r = append(*r, t...)
}

// DataTypeRouter returns the highest priority DataTypeRouter in the chain. A
// new router is created if one does not exist.
func (r *ChainRouter) DataTypeRouter() (t DataTypeRouter) {
	if !r.Find(&t) {
		t = DataTypeRouter{}
		r.Add(t)
	}
	return
}

// OrgRouter returns the highest priority OrgRouter in the chain.
func (r ChainRouter) OrgRouter() (t *OrgRouter) {
	r.Find(&t)
	return
}

// RoleRouter returns the highest priority RoleRouter in the chain. A new router
// is created if one does not exist.
func (r *ChainRouter) RoleRouter() (t RoleRouter) {
	if !r.Find(&t) {
		t = RoleRouter{}
		r.Add(t)
	}
	return
}

// STSRouter returns the highest priority STSRouter in the chain. A new router
// is created if one does not exist.
func (r *ChainRouter) STSRouter() (t STSRouter) {
	if !r.Find(&t) {
		t = STSRouter{}
		r.Add(t)
	}
	return
}

// UserRouter returns the highest priority UserRouter in the chain. A new router
// is created if one does not exist.
func (r *ChainRouter) UserRouter() (t UserRouter) {
	if !r.Find(&t) {
		t = UserRouter{}
		r.Add(t)
	}
	return
}

// Find assigns v, which must be a Router pointer, the highest priority router
// of the matching type.
func (r ChainRouter) Find(v interface{}) bool {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Ptr {
		panic("mock: " + t.String() + " is not a pointer")
	}
	if t = t.Elem(); !t.Implements(reflect.TypeOf((*Router)(nil)).Elem()) {
		panic("mock: " + t.String() + " is not a Router")
	}
	for i := len(r) - 1; i >= 0; i-- {
		if u := r[i]; reflect.TypeOf(u) == t {
			reflect.ValueOf(v).Elem().Set(reflect.ValueOf(u))
			return true
		}
	}
	return false
}

// Response contains values that are assigned to request Data and Error fields.
type Response struct {
	Out interface{}
	Err error
}

// DataTypeRouter handles requests based on the Data field type.
type DataTypeRouter map[reflect.Type]Response

// NewDataTypeRouter returns a router that will serve 'out' values to all
// requests with a matching data output type. All out values should be pointers
// to AWS SDK XyzOutput structs.
func NewDataTypeRouter(out ...interface{}) DataTypeRouter {
	r := make(DataTypeRouter, len(out))
	for _, v := range out {
		if _, ok := r[reflect.TypeOf(v)]; ok {
			panic(fmt.Sprintf("mock: %T already contains %T", r, out))
		}
		r.Set(v, nil)
	}
	return r
}

// Route implements the Router interface.
func (r DataTypeRouter) Route(q *Request) bool {
	_, ok := r[reflect.TypeOf(q.Data)]
	if ok {
		if err := r.Get(q.Data); err != nil {
			q.Error = err
		}
	}
	return ok
}

// Set allows the router to handle API requests with the given output type. If
// err is not nil, it will be used to set the request's Error field.
func (r DataTypeRouter) Set(out interface{}, err error) {
	t := reflect.TypeOf(out)
	if t.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("mock: %T is not a pointer", out))
	} else if s := t.Elem(); s.Kind() != reflect.Struct {
		panic(fmt.Sprintf("mock: %T is not a struct", s))
	} else if !strings.Contains(s.PkgPath(), "/aws-sdk-go-v2/") ||
		!strings.HasSuffix(s.Name(), "Output") {
		panic(fmt.Sprintf("mock: %T is not an AWS output struct", s))
	}
	r[t] = Response{out, err}
}

// Get copies a response value of the matching type into out and returns the
// associated error, if any.
func (r DataTypeRouter) Get(out interface{}) error {
	if v, ok := r[reflect.TypeOf(out)]; ok {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(v.Out).Elem())
		return v.Err
	}
	panic(fmt.Sprintf("mock: %T does not contain %T", r, out))
}
