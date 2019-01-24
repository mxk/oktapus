package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/mxk/go-cli"
	"github.com/mxk/oktapus/creds"
	"github.com/mxk/oktapus/op"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
)

var consoleCli = cli.Main.Add(&cli.Info{
	Name:    "console|cons",
	Usage:   "[options] [account-spec]",
	Summary: "Open AWS management console",
	MaxArgs: 1,
	New:     func() cli.Cmd { return &consoleCmd{Srv: "127.0.0.1:0"} },
})

var consoleEndpoints = map[string]console{
	endpoints.AwsPartitionID: {
		signin:  "signin.aws.amazon.com",
		console: "console.aws.amazon.com",
	},
	endpoints.AwsUsGovPartitionID: {
		signin:  "signin.amazonaws-us-gov.com",
		console: "console.amazonaws-us-gov.com",
	},
}

type consoleCmd struct {
	Region string `flag:"Open console to the <name>d region"`
	Srv    string `flag:"HTTP server listening <address>"`
	Switch bool   `flag:"Open 'Switch Role' page to avoid logging out"`
	URL    bool   `flag:"Write login url to stdout without opening it"`
	Spec   string
}

func (*consoleCmd) Info() *cli.Info { return consoleCli }

func (*consoleCmd) Help(w *cli.Writer) {
	w.Text(`
	Open AWS management console.

	This command requires an account-spec that matches exactly one account. The
	gateway credentials are used by default if no spec is provided. If these
	credentials belong to an IAM user, GetFederationToken API is called to get a
	temporary session. This session is not allowed to make IAM API calls, so
	that part of the console will be inaccessible.

	The login URL is normally opened via a redirect from an embedded HTTP
	server. Use -srv="" to disable the server and open the URL directly.

	Use -switch to change accounts via the "Switch Role" URL. This requires an
	existing console session that is allowed to assume the requested role.
	`)
	accountSpecHelp(w)
}

func (cmd *consoleCmd) Main(args []string) error {
	cmd.Spec = get(args, 0)
	return op.RunAndPrint(cmd)
}

func (cmd *consoleCmd) Run(ctx *op.Ctx) (interface{}, error) {
	ident := ctx.Ident()
	viaUser := ident.Type() == "user"
	cons := consoleEndpoints[ident.Partition()]
	if cons.signin == "" {
		return nil, errors.Errorf("unsupported partition %q", ident.Partition())
	}
	if cmd.Region == "" {
		cmd.Region = ctx.Cfg().Region
	}

	// AssumeRole creds
	if cmd.Spec != "" {
		acs, err := ctx.Match(cmd.Spec)
		if err != nil {
			return nil, err
		}
		if len(acs) != 1 {
			return nil, errors.Errorf("account-spec matched %d accounts", len(acs))
		}
		ac := acs[0]
		if cmd.Switch {
			role := ctx.Role().PathName()[1:]
			return cons.switchRole(ac.ID, ac.Name, role), nil
		}
		cr, err := ac.CredsProvider().Retrieve()
		if err != nil {
			return nil, errors.Wrap(err, "invalid credentials")
		}
		return cons.login(cr, viaUser, cmd.Region)
	}

	// Ctx creds
	if cmd.Switch {
		return nil, errors.New("-switch requires account-spec")
	}
	cr, err := ctx.Cfg().Credentials.Retrieve()
	if err != nil {
		return nil, errors.Wrap(err, "invalid credentials")
	}
	if viaUser {
		in := sts.GetFederationTokenInput{Name: aws.String(ident.Name())}
		out, err := sts.New(ctx.Cfg()).GetFederationTokenRequest(&in).Send()
		if err != nil {
			return nil, errors.Wrap(err, "GetFederationToken call failed")
		}
		cr = creds.FromSTS(out.Credentials)
	}
	return cons.login(cr, !viaUser, cmd.Region)
}

func (cmd *consoleCmd) Print(v interface{}) error {
	u, ok := v.(consoleURL)
	if !ok || u.Login == "" {
		return nil
	}
	if cmd.URL {
		fmt.Println(u.Login)
		return nil
	}
	if u.Logout != "" {
		if cmd.Srv != "" {
			return u.serve(cmd.Srv)
		}
		browser.OpenURL(u.Logout)
		time.Sleep(time.Second)
	}
	return errors.Wrap(browser.OpenURL(u.Login), "failed to open login url")
}

// console generates user login URLs using the steps described at:
// https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_providers_enable-console-custom-url.html
type console struct{ signin, console string }

// consoleURL contains user logout and login URLs.
type consoleURL struct{ Logout, Login string }

// colors are predefined on the switch role page. Custom colors not accepted.
var colors = []string{"F2B0A9", "FBBF93", "FAD791", "B7CA9D", "99BCE3"}

// switchRole returns a URL to switch user role without logging out.
func (f console) switchRole(accountID, accountName, role string) consoleURL {
	id, _ := strconv.ParseUint(accountID, 10, 64)
	return consoleURL{Login: f.signIn("/switchrole", url.Values{
		"account":     {accountID},
		"displayName": {accountName},
		"roleName":    {role},
		"color":       {colors[id%uint64(len(colors))]},
	})}
}

// login returns console login URL for the specified temporary creds. IAM user
// credentials must be converted to temporary ones via GetFederationToken API.
func (f console) login(cr aws.Credentials, setDur bool, region string) (consoleURL, error) {
	// Create getSigninToken request
	sess, err := json.Marshal(struct {
		AccessKeyID     string `json:"sessionId"`
		SecretAccessKey string `json:"sessionKey"`
		SessionToken    string `json:"sessionToken"`
	}{
		cr.AccessKeyID,
		cr.SecretAccessKey,
		cr.SessionToken,
	})
	if err != nil {
		return consoleURL{}, errors.Wrap(err, "failed to encode getSigninToken query")
	}
	q := url.Values{"Action": {"getSigninToken"}, "Session": {string(sess)}}
	if setDur {
		q.Set("SessionDuration", "43200")
	}
	req, err := http.NewRequest(http.MethodGet, f.signIn("/federation", q), nil)
	if err != nil {
		return consoleURL{}, errors.Wrap(err, "failed to create getSigninToken request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Oktapus/1.0")

	// Execute
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return consoleURL{}, errors.Wrap(err, "getSigninToken call failed")
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return consoleURL{}, errors.Errorf("getSigninToken returned %s", rsp.Status)
	}

	// Get SigninToken from the response
	var out struct{ SigninToken string }
	if err = json.NewDecoder(rsp.Body).Decode(&out); err != nil {
		return consoleURL{}, errors.Wrap(err, "failed to decode SigninToken")
	}
	if out.SigninToken == "" {
		return consoleURL{}, errors.New("AWS did not return SigninToken")
	}

	// Create login URL
	home := url.URL{
		Scheme:   "https",
		Host:     region + "." + f.console,
		Path:     "/console/home",
		RawQuery: url.Values{"region": {region}}.Encode(),
	}
	req.URL.RawQuery = url.Values{
		"Action":      {"login"},
		"Destination": {home.String() + "#"}, // Match AWS redirects
		"SigninToken": {out.SigninToken},
	}.Encode()
	return consoleURL{
		Logout: f.signIn("/oauth", url.Values{"Action": {"logout"}}),
		Login:  req.URL.String(),
	}, nil
}

func (f console) signIn(path string, query url.Values) string {
	u := url.URL{
		Scheme:   "https",
		Host:     f.signin,
		Path:     path,
		RawQuery: query.Encode(),
	}
	return u.String()
}

const loginTpl = `<!doctype html>
<html lang="en">
<head>
	<title>AWS Management Console</title>
</head>
<body>
	<p><a href="{{.Login}}" style="text-decoration:none">Loading...</a></p>
	<iframe src="{{.Logout}}" referrerpolicy="no-referrer" hidden="true"></iframe>
</body>
</html>
`

// serve does... something way too complicated just to avoid opening an extra
// browser tab.
//
// Unlike SAML logins, opening SigninToken URLs with an existing console session
// results in a "You must first log out before logging into a different AWS
// account" error. That's annoying. One solution is open the logout URL first,
// and then the login URL a second later, but that creates two browser tabs.
//
// This method starts an HTTP server at addr, opens the corresponding URL,
// responds to one request with a "Refresh" header containing the login URL and
// an iframe pointing at the logout URL, and terminates the server. The iframe
// is loaded first, logging the user out, refresh happens a second later, and
// everything stays in one tab.
func (u consoleURL) serve(addr string) error {
	// Render HTML page
	tpl, err := template.New("").Parse(loginTpl)
	if err != nil {
		return errors.Wrap(err, "failed to parse login page template")
	}
	var buf bytes.Buffer
	if err = tpl.Execute(&buf, u); err != nil {
		return errors.Wrap(err, "failed to render login page template")
	}

	// Start HTTP server
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to open server socket")
	}
	defer ln.Close()
	if err = browser.OpenURL("http://" + ln.Addr().String() + "/"); err != nil {
		return errors.Wrap(err, "failed to open server url")
	}
	html, done := make(chan []byte, 1), make(chan struct{})
	html <- buf.Bytes()
	s := http.Server{
		Addr: ln.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			select {
			case b := <-html:
				close(done)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Refresh", "1; url="+u.Login)
				w.Header().Set("Connection", "close")
				w.Write(b)
			default:
				http.NotFound(w, q)
			}
		}),
	}

	// Serve
	shutdown := make(chan error, 1)
	go func() {
		select {
		case <-done:
			shutdown <- s.Shutdown(context.Background())
		case <-time.After(10 * time.Second):
			s.Close()
			shutdown <- errors.New("server timeout")
		}
	}()
	if err = s.Serve(ln); err == http.ErrServerClosed {
		err = <-shutdown
	}
	return err
}
