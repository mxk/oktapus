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

// NewSession returns a client.ConfigProvider that does not use any environment
// variables or config files. If dfltRouters is true, the session's ChainRouter
// will contain all default router implementations.
func NewSession(dfltRouters bool) *Session {
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
			if !sess.Route(sess, r, api) {
				panic("mock: " + api + " not implemented")
			}
		},
	})

	// Configure default routers
	if dfltRouters {
		sess.Add(NewSTSRouter())
		sess.Add(NewOrgRouter())
	}
	return sess
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

// getReqAccountID returns the account ID for request q.
func getReqAccountID(q *request.Request) string {
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
