package okta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
)

// ErrRateLimit is returned when too many requests are sent.
var ErrRateLimit = errors.New("okta: request rate limit exceeded")

// Client provides access to Okta API.
type Client struct {
	BaseURL url.URL
	Client  *http.Client
	Sess    Session
}

// Session contains Okta session information.
type Session struct {
	ID        string
	Login     string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Status    string
}

// AppLink is an app that the user can access.
type AppLink struct {
	ID               string
	Label            string
	LinkURL          string
	LogoURL          string
	AppName          string
	AppInstanceID    string
	AppAssignmentID  string
	CredentialsSetup bool
	Hidden           bool
	SortOrder        int
}

// NewClient returns a new Okta API client for the specified subdomain (e.g.
// your-org.okta.com or dev-12345.oktapreview.com).
func NewClient(host string) *Client {
	return &Client{
		BaseURL: url.URL{Scheme: "https", Host: host, Path: "/api/v1/"},
		Client:  http.DefaultClient,
	}
}

// Authenticate performs user authentication and creates a new session.
func (c *Client) Authenticate(authn Authenticator) error {
	c.Sess = Session{}
	return authenticate(c, authn)
}

// ValidSession returns true if the client has a valid Okta session ID.
func (c *Client) ValidSession() bool {
	return c.Sess.ID != "" && c.Sess.ExpiresAt.After(
		fast.Time().Add(time.Minute))
}

// RefreshSession extends the expiration time of the current session.
func (c *Client) RefreshSession() error {
	var s Session
	ref := url.URL{Path: "sessions/me/lifecycle/refresh"}
	err := c.do(http.MethodPost, &ref, nil, &s)
	if err == nil {
		// The returned ID is an ExternalSessionID that does not replace the
		// original sid cookie.
		s.ID = c.Sess.ID
		c.Sess = s
	}
	return err
}

// CloseSession destroys the current session.
func (c *Client) CloseSession() error {
	ref := url.URL{Path: "sessions/me"}
	err := c.do(http.MethodDelete, &ref, nil, nil)
	c.Sess = Session{}
	return err
}

// AppLinks returns links for all applications assigned to the current user.
func (c *Client) AppLinks() ([]*AppLink, error) {
	var links []*AppLink
	ref := url.URL{Path: "users/" + c.Sess.UserID + "/appLinks"}
	err := c.do(http.MethodGet, &ref, nil, &links)
	return links, err
}

// OpenAWS returns SAML authentication data for the AWS app specified by
// appLink. If roleARN is specified, the matching AWS role is pre-selected.
func (c *Client) OpenAWS(appLink string, role arn.ARN) (*AWSAuth, error) {
	ref, err := url.Parse(appLink)
	if err != nil {
		return nil, err
	}
	req, err := c.req(http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	rsp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(rsp.Body)
	if rsp.StatusCode != http.StatusOK {
		if rsp.StatusCode == http.StatusTooManyRequests {
			return nil, ErrRateLimit
		}
		return nil, fmt.Errorf("okta: AWS app error (%s)", rsp.Status)
	}
	sa, err := samlAssertionFromHTML(rsp.Body)
	if err != nil {
		return nil, err
	}
	return newAWSAuth(sa, role)
}

// createSession converts session token into a cookie.
func (c *Client) createSession(sessionToken string) error {
	type in struct {
		SessionToken string `json:"sessionToken"`
	}
	var s Session
	ref := url.URL{Path: "sessions"}
	err := c.do(http.MethodPost, &ref, &in{sessionToken}, &s)
	if err == nil {
		c.Sess = s
	}
	return err
}

// do executes an Okta API call and decodes the response.
func (c *Client) do(method string, ref *url.URL, in, out interface{}) error {
	req, err := c.req(method, ref, in)
	if err == nil {
		var rsp *http.Response
		if rsp, err = c.Client.Do(req); err == nil {
			err = readResponse(rsp, out)
		}
	}
	return err
}

// req creates and configures a new API request.
func (c *Client) req(method string, ref *url.URL, body interface{}) (*http.Request, error) {
	if (ref.Scheme != "" && ref.Scheme != c.BaseURL.Scheme) ||
		(ref.Host != "" && ref.Host != c.BaseURL.Host) {
		return nil, fmt.Errorf("okta: invalid reference url (%s)", ref)
	}
	var r io.Reader
	if body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return nil, err
		}
		r = &buf
	}
	u := c.BaseURL.ResolveReference(ref).String()
	req, err := http.NewRequest(method, u, r)
	if err == nil {
		h := req.Header
		h.Set("Accept", "application/json")
		h.Set("Cache-Control", "no-cache")
		h.Set("User-Agent", "Oktapus/1.0")
		if body != nil {
			h.Set("Content-Type", "application/json; charset=UTF-8")
		}
		if c.Sess.ID != "" {
			req.AddCookie(&http.Cookie{Name: "sid", Value: c.Sess.ID})
		}
	}
	return req, err
}

// Error is an error report from Okta.
type Error struct {
	Code    string   `json:"errorCode"`
	Summary string   `json:"errorSummary"`
	Link    string   `json:"errorLink"`
	ReqID   string   `json:"errorId"`
	Causes  []*Error `json:"errorCauses"`
}

// Error implements error interface.
func (e *Error) Error() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		panic(err)
	}
	return b.String()
}

// readResponse reads JSON response from the server and decodes it into out.
// Server error codes (4xx/5xx) are returned as an *Error instance.
func readResponse(rsp *http.Response, out interface{}) error {
	defer closeBody(rsp.Body)
	if rsp.StatusCode == http.StatusNoContent {
		return nil
	} else if rsp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimit
	}
	ct, p, err := mime.ParseMediaType(rsp.Header.Get("Content-Type"))
	if err != nil {
		return err
	} else if ct != "application/json" {
		return fmt.Errorf("okta: unexpected content-type (%s)", ct)
	} else if cs, ok := p["charset"]; ok && !strings.EqualFold(cs, "UTF-8") {
		return fmt.Errorf("okta: unexpected charset (%s)", cs)
	}
	dec := json.NewDecoder(rsp.Body)
	if rsp.StatusCode >= 400 {
		e := new(Error)
		err := dec.Decode(e)
		if err == nil {
			err = e
		}
		return err
	}
	if out == nil {
		out = &struct{}{}
	}
	return dec.Decode(out)
}

// closeBody attempts to drain http.Response body before closing it to allow
// connection reuse (see https://github.com/google/go-github/pull/317 and
// https://github.com/golang/go/issues/20528).
func closeBody(body io.ReadCloser) {
	io.CopyN(ioutil.Discard, body, 4096)
	body.Close()
}
