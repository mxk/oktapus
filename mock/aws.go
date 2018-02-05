package mock

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/corehandlers"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// LogLevel is the log level for all mock sessions.
var LogLevel = aws.LogOff

// ServerFunc is a function that populates r.Data and r.Error, simulating an AWS
// response. It replaces the Send and all Unmarshal handlers in the session.
// Session serializes all requests, so ServerFuncs are allowed to modify router
// configuration.
type ServerFunc func(s *Session, r *request.Request)

// Router returns the server function that should handle the given request or
// nil if this request should be handled by another router.
type Router interface {
	Route(s *Session, r *request.Request, api string) ServerFunc
}

// Session is a client.ConfigProvider that uses routers and server functions to
// simulate AWS responses.
type Session struct {
	session.Session
	sync.Mutex
	ChainRouter
}

// NewSession returns a client.ConfigProvider that does not use any environment
// variables or config files.
func NewSession(stdRouters bool) *Session {
	cfg := &aws.Config{
		Credentials:      credentials.NewStaticCredentials("akid", "secret", ""),
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
		Fn: func(r *request.Request) {
			sess.Lock()
			defer sess.Unlock()
			r.Retryable = aws.Bool(false)
			api := r.ClientInfo.ServiceName + ":" + r.Operation.Name
			server := sess.Route(sess, r, api)
			if server == nil {
				panic("mock: " + api + " not implemented")
			}
			server(sess, r)
		},
	})

	// Configure standard routers
	if stdRouters {
		sess.Add(OrgRouter)
		sess.Add(NewSTSRouter())
	}
	return sess
}

// ChainRouter maintains a router chain and gives each router the option to
// handle each incoming request.
type ChainRouter struct{ chain []Router }

// Add inserts a new router into the chain, giving it priority over all other
// routers.
func (r *ChainRouter) Add(t Router) *ChainRouter {
	r.chain = append(r.chain, t)
	return r
}

// Route locates the router and server function to handle request r.
func (r *ChainRouter) Route(sess *Session, req *request.Request, api string) ServerFunc {
	for i := len(r.chain) - 1; i >= 0; i-- {
		if server := r.chain[i].Route(sess, req, api); server != nil {
			return server
		}
	}
	return nil
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
func (r *DataTypeRouter) Route(_ *Session, req *request.Request, _ string) ServerFunc {
	if _, ok := r.m[reflect.TypeOf(req.Data)]; ok {
		return r.serve
	}
	return nil
}

// serve is a ServerFunc.
func (r *DataTypeRouter) serve(_ *Session, req *request.Request) {
	if err := r.Get(req.Data); err != nil {
		req.Error = err
	}
}

// STSRouter handles STS API calls.
type STSRouter struct {
	ident map[string]*sts.GetCallerIdentityOutput
}

// NewSTSRouter returns an STSRouter configured to handle permanent credentials.
func NewSTSRouter() *STSRouter {
	return &STSRouter{ident: map[string]*sts.GetCallerIdentityOutput{
		"": &sts.GetCallerIdentityOutput{
			Account: aws.String("000000000000"),
			Arn:     aws.String("arn:aws:sts::000000000000:assumed-role/TestRole/TestSession"),
			UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:user@example.com"),
		}},
	}
}

// Set configures token identity.
func (r *STSRouter) Set(token string, out *sts.GetCallerIdentityOutput) {
	if out == nil {
		delete(r.ident, token)
	} else {
		r.ident[token] = out
	}
}

// Route implements the Router interface.
func (r *STSRouter) Route(_ *Session, req *request.Request, api string) ServerFunc {
	switch api {
	case "sts:GetCallerIdentity":
		return r.getCallerIdentity
	case "sts:AssumeRole":
		return r.assumeRole
	default:
		return nil
	}
}

func (r *STSRouter) getCallerIdentity(_ *Session, req *request.Request) {
	v, err := req.Config.Credentials.Get()
	if err != nil {
		req.Error = err
		return
	}
	out := r.ident[v.SessionToken]
	if out == nil {
		panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
	}
	*req.Data.(*sts.GetCallerIdentityOutput) = *out
}

func (r *STSRouter) assumeRole(_ *Session, req *request.Request) {
	in := req.Params.(*sts.AssumeRoleInput)
	role, err := arn.Parse(aws.StringValue(in.RoleArn))
	if err != nil {
		panic(err)
	}
	token := aws.StringValue(in.RoleArn)
	r.ident[token] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.AccountID),
		Arn:     in.RoleArn,
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:" + aws.StringValue(in.RoleSessionName)),
	}
	out := req.Data.(*sts.AssumeRoleOutput)
	out.Credentials = &sts.Credentials{
		AccessKeyId:     aws.String("AccessKeyId"),
		Expiration:      aws.Time(internal.Time().Add(time.Hour)),
		SecretAccessKey: aws.String("SecretAccessKey"),
		SessionToken:    aws.String(token),
	}
}

// OrgRouter contains API responses for a mock AWS organization.
var OrgRouter = NewDataTypeRouter(
	&orgs.DescribeOrganizationOutput{
		Organization: &orgs.Organization{
			Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
			FeatureSet:         aws.String(orgs.OrganizationFeatureSetAll),
			Id:                 aws.String("o-test"),
			MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
			MasterAccountEmail: aws.String("master@example.com"),
			MasterAccountId:    aws.String("000000000000"),
		},
	},
	&orgs.ListAccountsOutput{
		Accounts: []*orgs.Account{{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
			Email:           aws.String("master@example.com"),
			Id:              aws.String("000000000000"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodInvited),
			JoinedTimestamp: aws.Time(time.Unix(0, 0)),
			Name:            aws.String("master"),
			Status:          aws.String(orgs.AccountStatusActive),
		}, {
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000001"),
			Email:           aws.String("test1@example.com"),
			Id:              aws.String("000000000001"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(1, 0)),
			Name:            aws.String("test1"),
			Status:          aws.String(orgs.AccountStatusActive),
		}, {
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000002"),
			Email:           aws.String("test2@example.com"),
			Id:              aws.String("000000000002"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(2, 0)),
			Name:            aws.String("test2"),
			Status:          aws.String(orgs.AccountStatusSuspended),
		}},
	},
)

// disableHandlerList prevents a HandlerList from executing any handlers.
func disableHandlerList(name string, hl *request.HandlerList) {
	hl.PushFrontNamed(request.NamedHandler{
		Name: fmt.Sprintf("mock.%sHandler", name),
		Fn:   func(*request.Request) {},
	})
	hl.AfterEachFn = func(request.HandlerListRunItem) bool { return false }
}
