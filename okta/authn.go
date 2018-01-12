package okta

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Authenticator implements the user interface for multi-factor authentication.
type Authenticator interface {
	Username() (string, error)
	Password() (string, error)
	Select(c []Choice) (Choice, error)
	Input(c Choice) (string, error)
}

// Choice is a user-selectable item.
type Choice interface {
	Key() string
	Value() string
	Prompt() string
}

// Factor is a factor object returned by MFA_ENROLL, MFA_REQUIRED, or
// MFA_CHALLENGE authentication responses.
type Factor struct {
	ID         string
	FactorType string
	Provider   string
	VendorName string
	Profile    map[string]string
	Links      struct{ Verify *link } `json:"_links"`
	drv        mfaDriver
}

// Choice interface.
func (f *Factor) Key() string    { return f.ID }
func (f *Factor) Value() string  { return f.driver().name() }
func (f *Factor) Prompt() string { return f.driver().prompt(f) }

// driver returns the protocol driver for factor f.
func (f *Factor) driver() mfaDriver {
	if f.drv == nil {
		switch f.FactorType {
		case "question":
			f.drv = question{"Security Question"}
		case "call", "sms":
			name := driver("Phone Call")
			if f.FactorType == "sms" {
				name = "SMS"
			}
			if num := driver(f.Profile["phoneNumber"]); num != "" {
				f.drv = phone{name + " (" + num + ")"}
			} else {
				f.drv = phone{name}
			}
		case "token:software:totp":
			switch f.Provider {
			case "GOOGLE":
				f.drv = totp{"Google Authenticator"}
			}
		default:
			name := fmt.Sprintf("%s (%s)", f.FactorType, f.Provider)
			f.drv = unsupported{driver(name)}
		}
	}
	return f.drv
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
	Embedded     struct{ Factors []*Factor } `json:"_embedded"`
}

// link is a HAL entry in "_links".
type link struct{ Name, Href string }

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
			err = fmt.Errorf("okta: unsupported authn status %s", r.Status)
		}
	}
	return err
}

// primary validates user's primary password credential.
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
	choice := make([]Choice, 0, len(r.Embedded.Factors))
	var others []string
	for _, f := range r.Embedded.Factors {
		if d := f.driver(); d.supported() {
			choice = append(choice, f)
		} else {
			others = append(others, d.name())
		}
	}
	if len(choice) == 0 {
		return nil, fmt.Errorf("okta: no supported MFA methods (offered: %s)",
			strings.Join(others, ", "))
	}
	f, err := c.Select(choice)
	if err != nil {
		return nil, err
	}
	return f.(*Factor).driver().run(f.(*Factor), c, r)
}

// mfaDriver is a factor-specific driver interface.
type mfaDriver interface {
	supported() bool
	name() string
	prompt(f *Factor) string
	run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error)
}

// driver is the base mfaDriver implementation.
type driver string

func (d driver) supported() bool { return true }
func (d driver) name() string    { return string(d) }

// unsupported is a null driver for unsupported factors.
type unsupported struct{ driver }

func (d unsupported) supported() bool         { return false }
func (d unsupported) prompt(f *Factor) string { return "" }
func (d unsupported) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	return nil, fmt.Errorf("okta: unsupported factor (%s)", d.name())
}

// question implements security question verification protocol.
type question struct{ driver }

func (question) prompt(f *Factor) string {
	return f.Profile["questionText"]
}

func (question) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	var err error
	in := verifyInput{FID: f.ID, StateToken: r.StateToken}
	for in.Answer == "" {
		if in.Answer, err = c.Input(f); err != nil {
			return nil, err
		}
	}
	return verify(f, c, &in)
}

// phone implements phone call and SMS verification protocols.
type phone struct{ driver }

func (phone) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code sent to %q", f.Profile["phoneNumber"])
}

func (d phone) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	// Send empty passCode to request a new OTP
	in := verifyInput{FID: f.ID, StateToken: r.StateToken}
	if _, err := verify(f, c, &in); err != nil {
		return nil, err
	}
	return totp(d).run(f, c, r)
}

// totp implements time-based one-time password verification protocol.
type totp struct{ driver }

func (totp) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code for %q", f.Profile["credentialId"])
}

func (totp) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	var err error
	in := verifyInput{FID: f.ID, StateToken: r.StateToken}
	for in.PassCode == "" {
		if in.PassCode, err = c.Input(f); err != nil {
			return nil, err
		}
	}
	return verify(f, c, &in)
}

type verifyInput struct {
	FID        string `json:"fid"`
	StateToken string `json:"stateToken"`
	PassCode   string `json:"passCode,omitempty"`
	Answer     string `json:"answer,omitempty"`
}

// verify performs factor verification.
func verify(f *Factor, c *authnClient, in *verifyInput) (*authnResult, error) {
	ref, err := url.Parse(f.Links.Verify.Href)
	if err != nil {
		return nil, err
	}
	var out authnResult
	return &out, c.do(http.MethodPost, ref, in, &out)
}
