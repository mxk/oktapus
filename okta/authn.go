package okta

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
)

// Authenticator implements the user interface for multi-factor authentication.
type Authenticator interface {
	Username() (string, error)
	Password() (string, error)
	Select(c []Choice) (Choice, error)
	Input(c Choice) (string, error)
	Notify(format string, a ...interface{})
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
	ID         string                 `json:"id"`
	FactorType string                 `json:"factorType"`
	Provider   string                 `json:"provider"`
	VendorName string                 `json:"vendorName"`
	Profile    profile                `json:"profile"`
	Links      struct{ Verify *link } `json:"_links"`
	drv        mfaDriver
}

// profile contains token-specific profile information.
type profile struct {
	CredentialID   string
	QuestionText   string
	PhoneNumber    string
	PhoneExtension string
	Name           string
}

// Choice interface.
func (f *Factor) Key() string    { return f.ID }
func (f *Factor) Value() string  { return f.driver().name() }
func (f *Factor) Prompt() string { return f.driver().prompt(f) }

// driver returns the protocol driver for factor f.
func (f *Factor) driver() mfaDriver {
	if f.drv == nil {
		f.drv = newDriver(f)
	}
	return f.drv
}

// authnClient executes Okta's authentication flow.
type authnClient struct {
	*Client
	Authenticator
}

// response is the client's response to an authentication request.
type response struct {
	FID        string `json:"fid,omitempty"`
	StateToken string `json:"stateToken"`
	PassCode   string `json:"passCode,omitempty"`
	Answer     string `json:"answer,omitempty"`
}

// result is the result of an authentication request.
type result struct {
	StateToken   string                      `json:"stateToken"`
	SessionToken string                      `json:"sessionToken"`
	Status       string                      `json:"status"`
	FactorResult string                      `json:"factorResult"`
	Embedded     struct{ Factors []*Factor } `json:"_embedded"`
	Links        struct{ Next, Prev *link }  `json:"_links"`
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
			err = fmt.Errorf("okta: authentication failed (%s)", r.Status)
		}
	}
	return err
}

// primary validates user's primary password credential.
func (c *authnClient) primary() (*result, error) {
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
	var out result
	ref := url.URL{Path: "authn"}
	return &out, c.do(http.MethodPost, &ref, &in{user, pass}, &out)
}

// mfa performs multi-factor authentication.
func (c *authnClient) mfa(r *result) (*result, error) {
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
	r, err = f.(*Factor).driver().run(f.(*Factor), c, r)
	if err == nil && r.Status == "MFA_CHALLENGE" {
		in := response{StateToken: r.StateToken}
		r, err = c.nav(r.Links.Prev, &in)
	}
	return r, err
}

// nav sends a POST request to a HAL link.
func (c *authnClient) nav(l *link, in *response) (*result, error) {
	ref, err := url.Parse(l.Href)
	if err != nil {
		return nil, err
	}
	var out result
	return &out, c.do(http.MethodPost, ref, in, &out)
}

// mfaDriver is a factor-specific driver interface.
type mfaDriver interface {
	supported() bool
	name() string
	prompt(f *Factor) string
	run(f *Factor, c *authnClient, r *result) (*result, error)
}

func newDriver(f *Factor) mfaDriver {
	var name bytes.Buffer
	switch f.FactorType {
	case "question":
		name.WriteString("Security Question (")
		name.WriteString(f.Profile.QuestionText)
		name.WriteByte(')')
		return question{driver(name.Bytes())}
	case "call", "sms":
		if f.FactorType == "call" {
			name.WriteString("Phone Call")
		} else {
			name.WriteString("SMS")
		}
		if f.Profile.PhoneNumber != "" {
			name.WriteString(" (")
			name.WriteString(f.Profile.PhoneNumber)
			if f.Profile.PhoneExtension != "" {
				name.WriteString(" x")
				name.WriteString(f.Profile.PhoneExtension)
			}
			name.WriteByte(')')
		}
		return phone{driver(name.Bytes())}
	case "token:software:totp":
		switch f.Provider {
		case "GOOGLE":
			return totp{"Google Authenticator"}
		case "OKTA":
			return totp{"Okta Verify TOTP"}
		}
	case "push":
		if f.Provider == "OKTA" {
			name.WriteString("Okta Verify Push (")
			name.WriteString(f.Profile.Name)
			name.WriteByte(')')
			return push{driver(name.Bytes())}
		}
	}
	return unsupported{driver(fmt.Sprintf("%s (%s)", f.FactorType, f.Provider))}
}

// driver is the base mfaDriver implementation.
type driver string

func (d driver) supported() bool         { return true }
func (d driver) name() string            { return string(d) }
func (d driver) prompt(f *Factor) string { return "" }

// unsupported is a null driver for unsupported factors.
type unsupported struct{ driver }

func (d unsupported) supported() bool { return false }
func (d unsupported) run(f *Factor, c *authnClient, r *result) (*result, error) {
	return nil, fmt.Errorf("okta: unsupported factor (%s)", d.name())
}

// question implements security question verification protocol.
type question struct{ driver }

func (question) prompt(f *Factor) string {
	return f.Profile.QuestionText
}

func (question) run(f *Factor, c *authnClient, r *result) (*result, error) {
	var err error
	in := response{FID: f.ID, StateToken: r.StateToken}
	for in.Answer == "" {
		if in.Answer, err = c.Input(f); err != nil {
			return nil, err
		}
	}
	return c.nav(f.Links.Verify, &in)
}

// phone implements phone call and SMS verification protocols.
type phone struct{ driver }

func (phone) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code sent to %q", f.Profile.PhoneNumber)
}

func (d phone) run(f *Factor, c *authnClient, r *result) (*result, error) {
	// Send empty passCode to request a new OTP
	in := response{FID: f.ID, StateToken: r.StateToken}
	if _, err := c.nav(f.Links.Verify, &in); err != nil {
		return nil, err
	}
	return totp(d).run(f, c, r)
}

// totp implements time-based one-time password verification protocol.
type totp struct{ driver }

func (totp) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code for %q", f.Profile.CredentialID)
}

func (totp) run(f *Factor, c *authnClient, r *result) (*result, error) {
	var err error
	in := response{FID: f.ID, StateToken: r.StateToken}
	for in.PassCode == "" {
		if in.PassCode, err = c.Input(f); err != nil {
			return nil, err
		}
	}
	return c.nav(f.Links.Verify, &in)
}

// push implements push notification verification protocol.
type push struct{ driver }

func (push) run(f *Factor, c *authnClient, r *result) (*result, error) {
	in := response{FID: f.ID, StateToken: r.StateToken}
	r, err := c.nav(f.Links.Verify, &in)
	c.Notify("Waiting for approval from your %s... ", f.Profile.Name)
	for err == nil && r.FactorResult == "WAITING" {
		internal.Sleep(2 * time.Second)
		r, err = c.nav(r.Links.Next, &in)
	}
	c.Notify("%s\n", r.FactorResult)
	return r, err
}
