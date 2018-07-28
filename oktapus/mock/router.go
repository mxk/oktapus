package mock

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Router populates request Data and Error fields, simulating an AWS response.
// Route returns true if the request was handled or false if it should be given
// to the next router. Routers replace Send and all Unmarshal handlers in the
// session. The Session mutex is acquired while processing a request, so routers
// are allowed to modify the Session.
type Router interface {
	Route(q *aws.Request) bool
}

// ServerResult contains values that are assigned to request Data and Error
// fields.
type ServerResult struct {
	Out interface{}
	Err error
}

// ChainRouter maintains a router chain, passing requests to each router,
// starting with the last one, until the request is handled.
type ChainRouter []Router

// Add appends router t to the chain, giving it highest priority.
func (r *ChainRouter) Add(t Router) {
	*r = append(*r, t)
}

// DataTypeRouter returns the highest priority DataTypeRouter in the chain.
func (r ChainRouter) DataTypeRouter() (t DataTypeRouter) {
	r.Find(&t)
	return
}

// OrgsRouter returns the highest priority OrgsRouter in the chain.
func (r ChainRouter) OrgsRouter() (t OrgsRouter) {
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

// Route implements the Router interface.
func (r ChainRouter) Route(q *aws.Request) bool {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i].Route(q) {
			return true
		}
	}
	return false
}

// DataTypeRouter handles requests based on the Data field type.
type DataTypeRouter map[reflect.Type]ServerResult

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

// Set allows the router to handle API requests with the given output type. If
// err is not nil, it will be used to set the request's Error field.
func (r DataTypeRouter) Set(out interface{}, err error) {
	t := reflect.TypeOf(out)
	if t.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("mock: %T is not a pointer", out))
	} else if s := t.Elem(); s.Kind() != reflect.Struct {
		panic(fmt.Sprintf("mock: %T is not a struct", s))
	} else if !strings.Contains(s.PkgPath(), "/aws-sdk-go/") ||
		!strings.HasSuffix(s.Name(), "Output") {
		panic(fmt.Sprintf("mock: %T is not an AWS output struct", s))
	}
	r[t] = ServerResult{out, err}
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

// Route implements the Router interface.
func (r DataTypeRouter) Route(_ *Session, q *aws.Request, _ string) bool {
	_, ok := r[reflect.TypeOf(q.Data)]
	if ok {
		if err := r.Get(q.Data); err != nil {
			q.Error = err
		}
	}
	return ok
}
