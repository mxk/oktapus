package mock

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws/request"
)

// Router populates request Data and Error fields, simulating an AWS response.
// Route returns true if the request was handled or false if it should be given
// to the next router. Routers replace Send and all Unmarshal handlers in the
// session. Session serializes all requests, so routers are allowed to modify
// router configuration.
type Router interface {
	Route(s *Session, q *request.Request, api string) bool
}

// ServerResult contains values that are assigned to request Data and Error
// fields.
type ServerResult struct {
	Out interface{}
	Err error
}

// ChainRouter maintains a router chain and gives each router the option to
// handle each incoming request.
type ChainRouter struct{ chain []Router }

// NewChainRouter returns a router that will call the given routers in reverse
// order (i.e. the first router has the lowest priority).
func NewChainRouter(routers ...Router) *ChainRouter {
	return &ChainRouter{routers}
}

// Add inserts a new router into the chain, giving it highest priority.
func (r *ChainRouter) Add(t Router) *ChainRouter {
	r.chain = append(r.chain, t)
	return r
}

// GetType assigns v the highest priority router of the matching type.
func (r *ChainRouter) GetType(v interface{}) bool {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Ptr {
		panic("mock: v is not a pointer")
	}
	if t = t.Elem(); !t.Implements(reflect.TypeOf((*Router)(nil)).Elem()) {
		panic("mock: *v is not a Router")
	}
	for i := len(r.chain) - 1; i >= 0; i-- {
		if c := r.chain[i]; reflect.TypeOf(c) == t {
			reflect.ValueOf(v).Elem().Set(reflect.ValueOf(c))
			return true
		}
	}
	return false
}

// Route implements the Router interface.
func (r *ChainRouter) Route(s *Session, q *request.Request, api string) bool {
	for i := len(r.chain) - 1; i >= 0; i-- {
		if r.chain[i].Route(s, q, api) {
			return true
		}
	}
	return false
}

// DataTypeRouter handles requests based on the Data field type.
type DataTypeRouter struct{ m map[reflect.Type]ServerResult }

// NewDataTypeRouter returns a router that will serve 'out' values to all
// requests with a matching data output type. All out values should be pointers
// to AWS SDK *Output structs.
func NewDataTypeRouter(out ...interface{}) *DataTypeRouter {
	r := &DataTypeRouter{make(map[reflect.Type]ServerResult, len(out))}
	for _, v := range out {
		if _, ok := r.m[reflect.TypeOf(v)]; ok {
			panic(fmt.Sprintf("mock: %T already contains %T", r, out))
		}
		r.Set(v, nil)
	}
	return r
}

// Set allows the router to handle API requests with the given output type. If
// err is not nil, it will be used to set the request's Error field.
func (r *DataTypeRouter) Set(out interface{}, err error) {
	t := reflect.TypeOf(out)
	if t.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("mock: %T is not a pointer", out))
	} else if s := t.Elem(); s.Kind() != reflect.Struct {
		panic(fmt.Sprintf("mock: %T is not a struct", s))
	} else if !strings.Contains(s.PkgPath(), "/aws-sdk-go/") ||
		!strings.HasSuffix(s.Name(), "Output") {
		panic(fmt.Sprintf("mock: %T is not an AWS output struct", s))
	}
	r.m[t] = ServerResult{out, err}
}

// Get copies a response value of the matching type into out and returns the
// associated error, if any.
func (r *DataTypeRouter) Get(out interface{}) error {
	if v, ok := r.m[reflect.TypeOf(out)]; ok {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(v.Out).Elem())
		return v.Err
	}
	panic(fmt.Sprintf("mock: %T does not contain %T", r, out))
}

// Route implements the Router interface.
func (r *DataTypeRouter) Route(_ *Session, q *request.Request, _ string) bool {
	_, ok := r.m[reflect.TypeOf(q.Data)]
	if ok {
		if err := r.Get(q.Data); err != nil {
			q.Error = err
		}
	}
	return ok
}
