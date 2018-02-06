package mock

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Router returns the server function that should handle the given request or
// nil if this request should be handled by another router.
type Router interface {
	Route(s *Session, r *request.Request, api string) ServerFunc
}

// ServerFunc is a function that populates r.Data and r.Error, simulating an AWS
// response. It replaces the Send and all Unmarshal handlers in the session.
// Session serializes all requests, so ServerFuncs are allowed to modify router
// configuration.
type ServerFunc func(s *Session, r *request.Request)

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

// Route implements the Router interface.
func (r *ChainRouter) Route(sess *Session, req *request.Request, api string) ServerFunc {
	if r != nil {
		for i := len(r.chain) - 1; i >= 0; i-- {
			if server := r.chain[i].Route(sess, req, api); server != nil {
				return server
			}
		}
	}
	return nil
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

// STSRouter handles STS API calls. Key is SessionToken, which is the ARN of the
// assumed role.
type STSRouter map[string]*sts.GetCallerIdentityOutput

// NewSTSRouter returns a router configured to handle permanent credentials.
func NewSTSRouter() STSRouter {
	return map[string]*sts.GetCallerIdentityOutput{"": {
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:sts::000000000000:assumed-role/TestRole/TestSession"),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:user@example.com"),
	}}
}

// Route implements the Router interface.
func (r STSRouter) Route(_ *Session, req *request.Request, api string) ServerFunc {
	switch api {
	case "sts:AssumeRole":
		return r.assumeRole
	case "sts:GetCallerIdentity":
		return r.getCallerIdentity
	default:
		return nil
	}
}

// assumeRole handles sts:AssumeRole call.
func (r STSRouter) assumeRole(_ *Session, req *request.Request) {
	in := req.Params.(*sts.AssumeRoleInput)
	token := aws.StringValue(in.RoleArn)
	role, err := arn.Parse(token)
	if err != nil {
		panic(err)
	}
	i := strings.LastIndexByte(token, '/')
	name := token[i+1:]
	sess := aws.StringValue(in.RoleSessionName)
	sessArn := fmt.Sprintf("arn:aws:sts::%s:assumed-role/%s/%s", role.AccountID, name, sess)
	r[token] = &sts.GetCallerIdentityOutput{
		Account: aws.String(role.AccountID),
		Arn:     aws.String(sessArn),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:" + sess),
	}
	out := req.Data.(*sts.AssumeRoleOutput)
	out.Credentials = &sts.Credentials{
		AccessKeyId:     aws.String("AccessKeyId"),
		Expiration:      aws.Time(internal.Time().Add(time.Hour)),
		SecretAccessKey: aws.String("SecretAccessKey"),
		SessionToken:    aws.String(token),
	}
}

// getCallerIdentity handles sts:GetCallerIdentity call.
func (r STSRouter) getCallerIdentity(_ *Session, req *request.Request) {
	v, err := req.Config.Credentials.Get()
	if err != nil {
		req.Error = err
		return
	}
	out := r[v.SessionToken]
	if out == nil {
		panic(fmt.Sprintf("mock: invalid session token %q", v.SessionToken))
	}
	*req.Data.(*sts.GetCallerIdentityOutput) = *out
}

// AccountRouter implements account-specific Router interface.
type AccountRouter map[string]*ChainRouter

// Get returns the ChainRouter for the given account id, creating a new one if
// necessary.
func (r AccountRouter) Get(id string) *ChainRouter {
	id = AccountID(id)
	cr := r[id]
	if cr == nil {
		cr = NewChainRouter()
		r[id] = cr
	}
	return cr
}

// Route implements the Router interface.
func (r AccountRouter) Route(s *Session, req *request.Request, api string) ServerFunc {
	v, err := req.Config.Credentials.Get()
	if err != nil {
		panic(err)
	}
	id := "000000000000"
	if v.SessionToken != "" {
		id = AccountID(v.SessionToken)
	}
	return r[id].Route(s, req, api)
}

// RoleRouter implements IAM role API calls.
type RoleRouter map[string]*iam.Role

// Route implements the Router interface.
func (r RoleRouter) Route(s *Session, req *request.Request, api string) ServerFunc {
	switch api {
	case "iam:CreateRole":
		return r.createRole
	case "iam:GetRole":
		return r.getRole
	case "iam:UpdateRoleDescription":
		return r.updateRoleDescription
	default:
		return nil
	}
}

// createRole handles iam:CreateRole call.
func (r RoleRouter) createRole(_ *Session, req *request.Request) {
	in := req.Params.(*iam.CreateRoleInput)
	name := aws.StringValue(in.RoleName)
	if _, ok := r[name]; ok {
		panic("mock: role exists: " + name)
	}
	role := &iam.Role{
		AssumeRolePolicyDocument: in.AssumeRolePolicyDocument,
		Description:              in.Description,
		Path:                     in.Path,
		RoleName:                 in.RoleName,
	}
	r[name] = role
	cpy := *role
	cpy.Description = nil // Match AWS behavior
	req.Data.(*iam.CreateRoleOutput).Role = &cpy
}

// getRole handles iam:GetRole call.
func (r RoleRouter) getRole(_ *Session, req *request.Request) {
	name := aws.StringValue(req.Params.(*iam.GetRoleInput).RoleName)
	role := r[name]
	if role == nil {
		req.Error = invalidRole(name)
		return
	}
	cpy := *role
	req.Data.(*iam.GetRoleOutput).Role = &cpy
}

// updateRoleDescription handles iam:UpdateRoleDescription call.
func (r RoleRouter) updateRoleDescription(_ *Session, req *request.Request) {
	in := req.Params.(*iam.UpdateRoleDescriptionInput)
	name := aws.StringValue(in.RoleName)
	role := r[name]
	if role == nil {
		req.Error = invalidRole(name)
		return
	}
	role.Description = in.Description
	cpy := *role
	req.Data.(*iam.UpdateRoleDescriptionOutput).Role = &cpy
}

func invalidRole(name string) error {
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "Unknown role: "+name, nil)
	return awserr.NewRequestFailure(err, http.StatusNotFound, "")
}
