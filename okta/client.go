package okta

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
)

// Client provides access to Okta API.
type Client struct {
	BaseURL url.URL
	Client  *http.Client

	session   Session
	sidCookie string
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
	return authenticate(c, authn)
}

// Authenticated returns true if authentication is complete.
func (c *Client) Authenticated() bool {
	return c.sidCookie != "" && internal.Time().Before(c.session.ExpiresAt)
}

// RefreshSession extends the expiration time of the current session.
func (c *Client) RefreshSession() error {
	var out Session
	// Okta API documentation is not very good, to put it mildly. Both "Get
	// Current Session" and "Refresh Current Session" calls seem to extend
	// session expiration time. Both also return an ID that does not match our
	// sid cookie. Using the new ID for subsequent requests results in E0000007
	// "Not found" error. Okta support says this is an ExternalSessionID.
	ref := url.URL{Path: "sessions/me"}
	err := c.do(http.MethodGet, &ref, nil, &out)
	if err == nil {
		out.ID = c.session.ID
		out.ExpiresAt = out.ExpiresAt.Add(-time.Minute)
		err = c.setSession(&out)
	}
	return err
}

// CloseSession destroys the current session.
func (c *Client) CloseSession() error {
	ref := url.URL{Path: "sessions/me"}
	err := c.do(http.MethodDelete, &ref, nil, nil)
	c.setSession(nil)
	return err
}

// AppLinks returns links for all applications assigned to the current user.
func (c *Client) AppLinks() ([]*AppLink, error) {
	var out []*AppLink
	ref := url.URL{Path: "users/" + c.session.UserID + "/appLinks"}
	err := c.do(http.MethodGet, &ref, nil, &out)
	return out, err
}

// OpenAWS returns SAML authentication data for the AWS app specified by
// appLink. If roleARN is specified, the matching AWS role is pre-selected.
func (c *Client) OpenAWS(appLink, roleARN string) (*AWSAuth, error) {
	ref, err := url.Parse(appLink)
	if err != nil {
		return nil, err
	}
	req, err := c.newReq(http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	rsp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer internal.CloseBody(rsp.Body)
	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("okta: AWS app error (%s)", rsp.Status)
	}
	sa, err := samlAssertionFromHTML(rsp.Body)
	if err != nil {
		return nil, err
	}
	return newAWSAuth(sa, roleARN)
}

// state contains serialized Client state.
type state struct {
	BaseURL *url.URL
	Session *Session
}

// GobEncode implements gob.GobEncoder interface.
func (c *Client) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	s := state{BaseURL: &c.BaseURL, Session: &c.session}
	err := gob.NewEncoder(&buf).Encode(&s)
	return buf.Bytes(), err
}

// GobDecode implements gob.GobDecoder interface.
func (c *Client) GobDecode(b []byte) error {
	r := bytes.NewReader(b)
	s := state{}
	err := gob.NewDecoder(r).Decode(&s)
	if err == nil && c.BaseURL == *s.BaseURL {
		c.setSession(s.Session)
	}
	return err
}

// createSession converts session token into a cookie.
func (c *Client) createSession(sessionToken string) error {
	type in struct {
		SessionToken string `json:"sessionToken"`
	}
	var out Session
	ref := url.URL{Path: "sessions"}
	err := c.do(http.MethodPost, &ref, &in{sessionToken}, &out)
	if err == nil {
		out.ExpiresAt = out.ExpiresAt.Add(-time.Minute)
		err = c.setSession(&out)
	}
	return err
}

// setSession updates client session state.
func (c *Client) setSession(s *Session) error {
	if s != nil {
		if s.Status != "ACTIVE" {
			return fmt.Errorf("okta: inactive session (%s)", s.Status)
		}
		c.session = *s
	} else {
		c.session = Session{}
	}
	c.sidCookie = sidCookie(c.session.ID)
	return nil
}

// do executes an Okta API call and decodes the response.
func (c *Client) do(method string, ref *url.URL, in, out interface{}) error {
	req, err := c.newReq(method, ref, in)
	if err == nil {
		var rsp *http.Response
		if rsp, err = c.Client.Do(req); err == nil {
			err = readResponse(rsp, out)
		}
	}
	return err
}

// newReq creates and configures a new API request.
func (c *Client) newReq(method string, ref *url.URL, body interface{}) (*http.Request, error) {
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
		h.Set("User-Agent", internal.UserAgent)
		if body != nil {
			h.Set("Content-Type", "application/json;charset=UTF-8")
		}
		if c.sidCookie != "" {
			h.Set("Cookie", c.sidCookie)
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

// Error implements the error interface.
func (e *Error) Error() string {
	return internal.JSON(e)
}

const responseDebug = false

// readResponse reads JSON response from the server and decodes it into out.
// Server error codes (4xx/5xx) are returned as an *Error instance.
func readResponse(rsp *http.Response, out interface{}) error {
	defer internal.CloseBody(rsp.Body)
	if rsp.StatusCode == http.StatusNoContent {
		return nil
	}
	ct, p, err := mime.ParseMediaType(rsp.Header.Get("Content-Type"))
	if err != nil {
		return err
	} else if ct != "application/json" {
		return fmt.Errorf("okta: unexpected content-type (%s)", ct)
	} else if cs, ok := p["charset"]; ok && !strings.EqualFold(cs, "UTF-8") {
		return fmt.Errorf("okta: unexpected charset (%s)", cs)
	}
	var dec *json.Decoder
	//noinspection GoBoolExpressions
	if responseDebug {
		b, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err = json.Indent(&buf, bytes.TrimSpace(b), "", "  "); err != nil {
			return err
		}
		fmt.Printf("%s\n%s\n", rsp.Status, buf.Bytes())
		dec = json.NewDecoder(bytes.NewReader(b))
	} else {
		dec = json.NewDecoder(rsp.Body)
	}
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

// sidCookie encodes sid for use in the Cookie header.
func sidCookie(sid string) string {
	if sid == "" {
		return ""
	}
	c := http.Cookie{Name: "sid", Value: sid}
	return c.String()
}
