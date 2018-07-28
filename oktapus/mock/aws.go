package mock

import (
	"sync"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/awsmock"
	"github.com/aws/aws-sdk-go-v2/aws"
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
	aws.Config
	ChainRouter
	mu sync.Mutex
}

// NewSession returns a mock client.ConfigProvider configured with default
// routers request routers.
func NewSession() *Session {
	s := &Session{
		ChainRouter: ChainRouter{NewSTSRouter(""), NewOrgsRouter()},
	}
	s.Config = awsmock.Config(func(q *aws.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.Route(q) {
			api := q.Metadata.ServiceName + ":" + q.Operation.Name
			panic("mock: " + api + " not implemented")
		}
	})
	s.Config.LogLevel = LogLevel
	s.Config.Credentials = aws.NewStaticCredentialsProvider(
		AccessKeyID, SecretAccessKey, "")
	return s
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
	if id = arn.ARN(id).Account(); len(id) != 12 {
		panic("mock: invalid arn: " + orig)
	}
	return id
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
func reqAccountID(q *aws.Request) string {
	cr, err := q.Config.Credentials.Retrieve()
	if err != nil {
		panic(err)
	}
	if cr.SessionToken != "" {
		return AccountID(cr.SessionToken)
	}
	return "000000000000"
}
