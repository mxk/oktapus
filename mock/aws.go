package mock

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/corehandlers"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
)

// LogLevel is the log level for all mock sessions.
var LogLevel = aws.LogOff

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
		}, {
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000003"),
			Email:           aws.String("test3@example.com"),
			Id:              aws.String("000000000003"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(3, 0)),
			Name:            aws.String("test3"),
			Status:          aws.String(orgs.AccountStatusActive),
		}},
	},
)

// IAMRouter handles IAM requests for the mock organization.
var IAMRouter = AccountRouter(map[string]*ChainRouter{
	"000000000000": NewChainRouter(RoleRouter{}),
	"000000000001": NewChainRouter(RoleRouter{}),
	"000000000002": NewChainRouter(RoleRouter{}),
	"000000000003": NewChainRouter(RoleRouter{}),
})

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
			server := sess.Route(sess, r, api)
			if server == nil {
				panic("mock: " + api + " not implemented")
			}
			server(sess, r)
		},
	})

	// Configure default routers
	if dfltRouters {
		sess.Add(OrgRouter)
		sess.Add(NewSTSRouter())
		sess.Add(IAMRouter)
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

// disableHandlerList prevents a HandlerList from executing any handlers.
func disableHandlerList(name string, hl *request.HandlerList) {
	hl.PushFrontNamed(request.NamedHandler{
		Name: fmt.Sprintf("mock.%sHandler", name),
		Fn:   func(*request.Request) {},
	})
	hl.AfterEachFn = func(request.HandlerListRunItem) bool { return false }
}
