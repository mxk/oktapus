package mock

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/corehandlers"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
)

// LogLevel is the log level for all mock sessions.
var LogLevel = aws.LogOff

// ServerFunc is a function that populates r.Data and r.Error, simulating an AWS
// response. It replaces the Send and all Unmarshal handlers in the session.
type ServerFunc func(r *request.Request)

// Router returns the server function that should handle the given request or
// nil if this request should be handled by another router.
type Router interface {
	Route(api string, r *request.Request) ServerFunc
}

// Session is a client.ConfigProvider that uses routers and server functions to
// simulate AWS responses.
type Session struct {
	session.Session
	AWS
}

// NewSession returns a client.ConfigProvider that does not use any environment
// variables or config files.
func NewSession() *Session {
	cfg := &aws.Config{
		Credentials:      credentials.AnonymousCredentials,
		EndpointResolver: endpoints.DefaultResolver(),
		LogLevel:         &LogLevel,
		Logger:           aws.NewDefaultLogger(),
		MaxRetries:       aws.Int(0),
	}
	sess := &Session{
		Session: session.Session{Config: cfg, Handlers: defaults.Handlers()},
	}
	sess.Session = *sess.Session.Copy() // Run initHandlers

	// Remove/disable all data-related handlers
	sess.Handlers.Send.Remove(corehandlers.SendHandler)
	sess.Handlers.Send.Remove(corehandlers.ValidateReqSigHandler)
	sess.Handlers.ValidateResponse.Remove(corehandlers.ValidateResponseHandler)
	disableHandlerList("Unmarshal", &sess.Handlers.Unmarshal)
	disableHandlerList("UnmarshalMeta", &sess.Handlers.UnmarshalMeta)
	disableHandlerList("UnmarshalError", &sess.Handlers.UnmarshalError)

	// Install mock handler
	sess.Handlers.Send.PushBackNamed(request.NamedHandler{
		Name: "mock.SendHandler",
		Fn:   sess.AWS.do,
	})
	return sess
}

// AWS handles requests as though they were sent to AWS servers. It maintains a
// router chain and gives each router the option to handle each incoming
// request.
type AWS struct{ routers []Router }

// Add inserts a new router into the chain, giving it priority over any existing
// routers.
func (s *AWS) Add(r Router) *AWS {
	s.routers = append(s.routers, r)
	return s
}

// do locates the router and server function to handle request r.
func (s *AWS) do(r *request.Request) {
	api := r.ClientInfo.ServiceName + ":" + r.Operation.Name
	for i := len(s.routers) - 1; i >= 0; i-- {
		if server := s.routers[i].Route(api, r); server != nil {
			server(r)
			return
		}
	}
	panic("mock: " + api + " not implemented")
}

// ServerResult contains values that are assigned to request Data and Error
// fields.
type ServerResult struct {
	Out interface{}
	Err error
}

// DataTypeRouter handles requests based on the Data field type.
type DataTypeRouter struct {
	m map[reflect.Type]ServerResult
}

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
func (r *DataTypeRouter) Route(api string, req *request.Request) ServerFunc {
	if _, ok := r.m[reflect.TypeOf(req.Data)]; ok {
		return r.serve
	}
	return nil
}

// serve is a ServerFunc.
func (r *DataTypeRouter) serve(req *request.Request) {
	if err := r.Get(req.Data); err != nil {
		req.Error = err
	}
}

// disableHandlerList prevents a HandlerList from executing any handlers.
func disableHandlerList(name string, hl *request.HandlerList) {
	hl.PushFrontNamed(request.NamedHandler{
		Name: fmt.Sprintf("mock.%sHandler", name),
		Fn:   func(*request.Request) {},
	})
	hl.AfterEachFn = func(request.HandlerListRunItem) bool { return false }
}
