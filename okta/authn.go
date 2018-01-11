package okta

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Authenticator is a data source for all authentication steps.
type Authenticator interface {
	Username() (string, error)
	Password() (string, error)
	SelectFactor(f []*Factor) (*Factor, error)
	Challenge(f *Factor) (string, error)
}

// authnClient executes Okta's authentication flow.
type authnClient struct {
	*Client
	Authenticator
}

// authnResult is the result of the most recent authentication request.
type authnResult struct {
	StateToken   string
	SessionToken string
	Status       string
	Links        map[string]*link            `json:"_links"`
	Embedded     struct{ Factors []*Factor } `json:"_embedded"`
}

// link is a HAL entry in "_links".
type link struct {
	Name string
	Href string
}

// authenticate validates user credentials with Okta.
func authenticate(c *Client, authn Authenticator) error {
	ac := authnClient{c, authn}
	r, err := ac.primary()
	for err == nil {
		switch r.Status {
		case "SUCCESS":
			return c.createSession(r.SessionToken)
		case "MFA_REQUIRED":
			r, err = ac.mfa(r)
		default:
			return fmt.Errorf("okta: authn status %s", r.Status)
		}
	}
	return err
}

// primary validates the user's primary password credential.
func (c *authnClient) primary() (*authnResult, error) {
	user, err := c.Username()
	if err != nil {
		return nil, err
	}
	pass, err := c.Password()
	if err != nil {
		return nil, err
	}
	type in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var out authnResult
	ref := url.URL{Path: "authn"}
	return &out, c.do(http.MethodPost, &ref, &in{user, pass}, &out)
}

// mfa performs multi-factor authentication.
func (c *authnClient) mfa(r *authnResult) (*authnResult, error) {
	supported := make([]*Factor, 0, len(r.Embedded.Factors))
	for _, f := range r.Embedded.Factors {
		if f.Info().Supported {
			supported = append(supported, f)
		}
	}
	if len(supported) == 0 {
		return nil, errors.New("okta: no supported MFA factors")
	}
	f, err := c.SelectFactor(supported)
	if err != nil {
		return nil, err
	}
	return f.Info().protocol(c, f, r)
}

// Factor is a factor object returned by MFA_ENROLL, MFA_REQUIRED, or
// MFA_CHALLENGE authentication responses.
type Factor struct {
	ID         string
	FactorType string
	Provider   string
	VendorName string
	Links      map[string]*link `json:"_links"`
}

// Info returns factor metadata.
func (f *Factor) Info() FactorInfo {
	switch f.FactorType {
	case "token:software:totp":
		switch f.Provider {
		case "GOOGLE":
			return googleAuth
		}
	}
	return FactorInfo{Name: fmt.Sprintf("%s (%s)", f.FactorType, f.Provider)}
}

// FactorInfo describes a specific factor type.
type FactorInfo struct {
	Supported bool
	Name      string
	Prompt    string

	protocol protoFunc
}

var (
	googleAuth = FactorInfo{
		Supported: true,
		Name:      "Google Authenticator",
		Prompt:    "Verification code",
		protocol:  totp,
	}
)

type protoFunc func(c *authnClient, f *Factor, r *authnResult) (*authnResult, error)

// totp implements time-based one-time password protocol.
func totp(c *authnClient, f *Factor, r *authnResult) (*authnResult, error) {
	passCode, err := c.Challenge(f)
	if err != nil {
		return nil, err
	}
	type in struct {
		FID        string `json:"fid"`
		StateToken string `json:"stateToken"`
		PassCode   string `json:"passCode"`
	}
	var out authnResult
	ref := url.URL{Path: "authn/factors/" + f.ID + "/verify"}
	req := in{f.ID, r.StateToken, passCode}
	return &out, c.do(http.MethodPost, &ref, &req, &out)
}
