package mock

import (
	"fmt"
	"strings"
	"sync"

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

// Session is a client.ConfigProvider that uses routers and server functions to
// simulate AWS responses.
type Session struct {
	session.Session
	sync.Mutex
	ChainRouter
}

// NewSession returns a mock client.ConfigProvider configured with default
// routers request routers.
func NewSession() *Session {
	cfg := &aws.Config{
		Credentials:      credentials.NewStaticCredentials("akid", "secret", ""),
		EndpointResolver: endpoints.DefaultResolver(),
		LogLevel:         &LogLevel,
		Logger:           aws.NewDefaultLogger(),
		MaxRetries:       aws.Int(0),
	}
	s := &Session{
		Session: session.Session{Config: cfg, Handlers: defaults.Handlers()},
	}
	s.Session = *s.Session.Copy() // Run initHandlers

	// Remove/disable all data-related handlers
	s.Handlers.Send.Remove(corehandlers.SendHandler)
	s.Handlers.Send.Remove(corehandlers.ValidateReqSigHandler)
	s.Handlers.ValidateResponse.Remove(corehandlers.ValidateResponseHandler)
	disableHandlerList("Unmarshal", &s.Handlers.Unmarshal)
	disableHandlerList("UnmarshalMeta", &s.Handlers.UnmarshalMeta)
	disableHandlerList("UnmarshalError", &s.Handlers.UnmarshalError)

	// Install mock handler
	s.Handlers.Send.PushBackNamed(request.NamedHandler{
		Name: "mock.SendHandler",
		Fn: func(q *request.Request) {
			s.Lock()
			defer s.Unlock()
			q.Retryable = aws.Bool(false)
			api := q.ClientInfo.ServiceName + ":" + q.Operation.Name
			if !s.Route(s, q, api) {
				panic("mock: " + api + " not implemented")
			}
		},
	})

	// Configure default routers
	s.Add(NewSTSRouter())
	s.Add(NewOrgRouter())
	return s
}

// AccountID returns the 12-digit account ID from id, which may an account ID
// without leading zeros or an ARN.
func AccountID(id string) string {
	if len(id) == 12 {
		return id
	}
	if len(id) < 12 {
		var buf [12]byte
		n := copy(buf[:], "000000000000"[:len(buf)-len(id)])
		copy(buf[n:], id)
		return string(buf[:])
	}
	orig := id
	for i := 4; i > 0; i-- {
		j := strings.IndexByte(id, ':')
		if j == -1 {
			panic("mock: invalid arn: " + orig)
		}
		id = id[j+1:]
	}
	if strings.IndexByte(id, ':') != 12 {
		panic("mock: invalid arn: " + orig)
	}
	return id[:12]
}

// UserARN returns an IAM user ARN.
func UserARN(account, name string) string {
	return iamARN(account, "user", name)
}

// RoleARN returns an IAM role ARN.
func RoleARN(account, name string) string {
	return iamARN(account, "role", name)
}

// PolicyARN returns an IAM policy ARN.
func PolicyARN(account, name string) string {
	return iamARN(account, "policy", name)
}

// iamARN constructs an IAM ARN.
func iamARN(account, typ, name string) string {
	return "arn:aws:iam::" + account + ":" + typ + "/" + name
}

// reqAccountID returns the account ID for request q.
func reqAccountID(q *request.Request) string {
	v, err := q.Config.Credentials.Get()
	if err != nil {
		panic(err)
	}
	if v.SessionToken != "" {
		return AccountID(v.SessionToken)
	}
	return "000000000000"
}

// disableHandlerList prevents a HandlerList from executing any handlers.
func disableHandlerList(name string, hl *request.HandlerList) {
	hl.PushFrontNamed(request.NamedHandler{
		Name: fmt.Sprintf("mock.%sHandler", name),
		Fn:   func(*request.Request) {},
	})
	hl.AfterEachFn = func(request.HandlerListRunItem) bool { return false }
}
