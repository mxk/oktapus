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

// Choice is a user-selectable item, such as a factor or a security question.
type Choice interface {
	Key() string
	Value() string
	Prompt() string
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

// Factor is a factor object returned by MFA_ENROLL, MFA_REQUIRED, or
// MFA_CHALLENGE authentication responses.
type Factor struct {
	ID         string
	FactorType string
	Provider   string
	VendorName string
	Profile    map[string]string
	Links      map[string]*link `json:"_links"`
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
		case "token:software:totp":
			switch f.Provider {
			case "GOOGLE":
				f.drv = totp{"Google Authenticator"}
			}
		case "sms":
			if phone := driver(f.Profile["phoneNumber"]); phone != "" {
				f.drv = sms{"SMS (" + phone + ")"}
			} else {
				f.drv = sms{"SMS"}
			}
		default:
			name := fmt.Sprintf("%s (%s)", f.FactorType, f.Provider)
			f.drv = unsupported{driver(name)}
		}
	}
	return f.drv
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

type passCodeInput struct {
	FID        string `json:"fid"`
	StateToken string `json:"stateToken"`
	PassCode   string `json:"passCode,omitempty"`
}

// totp implements time-based one-time password verification protocol.
type totp struct{ driver }

func (d totp) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code for %q", f.Profile["credentialId"])
}

func (d totp) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	ref, err := url.Parse(f.Links["verify"].Href)
	if err != nil {
		return nil, err
	}
	in := passCodeInput{FID: f.ID, StateToken: r.StateToken}
	for in.PassCode == "" {
		if in.PassCode, err = c.Input(f); err != nil {
			return nil, err
		}
	}
	var out authnResult
	return &out, c.do(http.MethodPost, ref, &in, &out)
}

// sms implements SMS verification protocol.
type sms struct{ driver }

func (d sms) prompt(f *Factor) string {
	return fmt.Sprintf("Verification code sent to %q", f.Profile["phoneNumber"])
}

func (d sms) run(f *Factor, c *authnClient, r *authnResult) (*authnResult, error) {
	ref, err := url.Parse(f.Links["verify"].Href)
	if err != nil {
		return nil, err
	}
	// Send empty passCode to request a new OTP
	in := passCodeInput{FID: f.ID, StateToken: r.StateToken}
	if err := c.do(http.MethodPost, ref, &in, nil); err != nil {
		return nil, err
	}
	return totp(d).run(f, c, r)
}
