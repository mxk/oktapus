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

// Example access key.
const (
	AccessKeyID     = "AKIAIOSFODNN7EXAMPLE"
	SecretAccessKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
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
	creds := credentials.NewStaticCredentials(AccessKeyID, SecretAccessKey, "")
	cfg := &aws.Config{
		Credentials:      creds,
		EndpointResolver: endpoints.DefaultResolver(),
		LogLevel:         &LogLevel,
		Logger:           aws.NewDefaultLogger(),
		MaxRetries:       aws.Int(0),
	}
	//noinspection GoStructInitializationWithoutFieldNames
	s := &Session{
		Session:     session.Session{cfg, defaults.Handlers()},
		ChainRouter: ChainRouter{NewSTSRouter(""), NewOrgsRouter()},
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
		Fn:   s.sendHandler,
	})
	return s
}

// sendHandler passes request q through the router chain.
func (s *Session) sendHandler(q *request.Request) {
	s.Lock()
	defer s.Unlock()
	q.Retryable = aws.Bool(false)
	api := q.ClientInfo.ServiceName + ":" + q.Operation.Name
	if !s.Route(s, q, api) {
		panic("mock: " + api + " not implemented")
	}
}

// AccountID returns the 12-digit account ID from id, which may an account ID
// with or without leading zeros, or an ARN.
func AccountID(id string) string {
	if len(id) == 12 {
		return id
	}
	if len(id) < 12 {
		for i := len(id) - 1; i >= 0; i-- {
			if c := id[i]; c < '0' || '9' < c {
				panic("mock: invalid account id: " + id)
			}
		}
		var buf [12]byte
		n := copy(buf[:], "000000000000"[:len(buf)-len(id)])
		copy(buf[n:], id)
		return string(buf[:])
	}
	orig := id
	for i := 4; i > 0; i-- {
		id = id[strings.IndexByte(id, ':')+1:]
	}
	if strings.IndexByte(id, ':') != 12 {
		panic("mock: invalid arn: " + orig)
	}
	return id[:12]
}

// AssumedRoleARN returns an STS assumed role ARN.
func AssumedRoleARN(account, role, roleSessionName string) string {
	return "arn:aws:sts::" + AccountID(account) + ":assumed-role/" + role +
		"/" + roleSessionName
}

// PolicyARN returns an IAM policy ARN.
func PolicyARN(account, name string) string {
	return iamARN(account, "policy", name)
}

// RoleARN returns an IAM role ARN.
func RoleARN(account, name string) string {
	return iamARN(account, "role", name)
}

// UserARN returns an IAM user ARN.
func UserARN(account, name string) string {
	return iamARN(account, "user", name)
}

// iamARN constructs an IAM ARN.
func iamARN(account, typ, name string) string {
	return "arn:aws:iam::" + AccountID(account) + ":" + typ + "/" + name
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
