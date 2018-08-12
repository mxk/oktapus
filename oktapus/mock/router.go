package mock

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// Request wraps aws.Request to provide additional functionality.
type Request struct {
	*aws.Request
	Ctx arn.Ctx
}

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

// AccountRouter returns the highest priority AccountRouter in the chain.
func (r ChainRouter) AccountRouter() (t AccountRouter) {
	r.Find(&t)
	return
}

// DataTypeRouter returns the highest priority DataTypeRouter in the chain.
func (r ChainRouter) DataTypeRouter() (t DataTypeRouter) {
	r.Find(&t)
	return
}

// RoleRouter returns the highest priority RoleRouter in the chain.
func (r ChainRouter) RoleRouter() (t RoleRouter) {
	r.Find(&t)
	return
}

// STSRouter returns the highest priority STSRouter in the chain.
func (r ChainRouter) STSRouter() (t STSRouter) {
	r.Find(&t)
	return
}

// UserRouter returns the highest priority UserRouter in the chain.
func (r ChainRouter) UserRouter() (t UserRouter) {
	r.Find(&t)
	return
}

// Find assigns v, which must be a Router pointer, the highest priority router
// of the matching type.
func (r ChainRouter) Find(v interface{}) bool {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Ptr {
		panic("mock: v is not a pointer")
	}
	if t = t.Elem(); !t.Implements(reflect.TypeOf((*Router)(nil)).Elem()) {
		panic("mock: *v is not a Router")
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
