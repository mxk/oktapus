package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/LuminalHQ/oktapus/internal"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/pkg/browser"
)

const (
	fedURL    = "https://signin.aws.amazon.com/federation"
	roleURL   = "https://signin.aws.amazon.com/switchrole"
	logoutURL = "https://signin.aws.amazon.com/oauth?Action=logout"
	consURL   = "https://console.aws.amazon.com/"
)

func init() {
	register(&cmdInfo{
		names:   []string{"console", "cons"},
		summary: "Open AWS management console",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
		new:     func() Cmd { return &console{Name: "console"} },
	})
}

type console struct {
	Name
	SwitchRole bool
	Spec       string
}

func (cmd *console) Help(w *bufio.Writer) {
	writeHelp(w, `
		Open AWS management console.

		This command accepts the same account-spec as all other commands, but it
		must match exactly one account.

		Currently, AWS does not automatically log you out of an existing session
		when a new one is opened. As a result, opening a new session results in
		a message telling you to log out of the other one first. To bypass this,
		the command first opens a logout URL, followed by the console login URL.

		Alternatively, you can use -sr option to change accounts via the AWS
		"Switch Role" function. This requires you to be logged into a session
		that is allowed to assume the requested role.
	`)
}

func (cmd *console) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.SwitchRole, "sr", false,
		`Open "Switch Role" page to avoid logging out`)
}

func (cmd *console) Run(ctx *Ctx, args []string) error {
	// TODO: Call creds command instead and open from the current process?
	cmd.Spec = args[0]
	_, err := ctx.Call(cmd)
	return err
}

func (cmd *console) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	if len(acs) != 1 {
		return nil, fmt.Errorf("account spec %q matched %d accounts",
			cmd.Spec, len(acs))
	}
	ac := acs[0]
	if cmd.SwitchRole {
		return nil, cmd.switchRole(ac.ID, ac.Name, ctx.AWS().CommonRole)
	}
	c, err := ac.Creds(false)
	if err == nil {
		err = cmd.open(c.Value)
	}
	return nil, err
}

// colors are predefined on the switch role page. Custom colors not accepted.
var colors = []string{"F2B0A9", "FBBF93", "FAD791", "B7CA9D", "99BCE3"}

// switchRole allows the user to access another account without logging out.
func (*console) switchRole(accountID, accountName, role string) error {
	id, _ := strconv.ParseInt(accountID, 10, 64)
	q := make(url.Values)
	q.Set("account", accountID)
	q.Set("roleName", role)
	q.Set("displayName", accountName)
	q.Set("color", colors[rand.New(rand.NewSource(id)).Intn(len(colors))])
	return browser.OpenURL(roleURL + "?" + q.Encode())
}

// open launches AWS management console using the process described here:
// https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_providers_enable-console-custom-url.html
func (*console) open(cr credentials.Value) error {
	in := struct {
		AccessKeyID     string `json:"sessionId"`
		SecretAccessKey string `json:"sessionKey"`
		SessionToken    string `json:"sessionToken"`
	}{
		cr.AccessKeyID,
		cr.SecretAccessKey,
		cr.SessionToken,
	}
	sess, err := json.Marshal(&in)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fedURL, nil)
	if err != nil {
		return err
	}
	h := req.Header
	h.Set("Accept", "application/json")
	h.Set("User-Agent", internal.UserAgent)

	// TODO: Adding SessionDuration with any value causes the request to fail.
	// Try using GetFederationToken?
	q := make(url.Values)
	q.Set("Action", "getSigninToken")
	q.Set("Session", string(sess))
	req.URL.RawQuery = q.Encode()

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer internal.CloseBody(rsp.Body)
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %s", rsp.Status)
	}
	var out struct{ SigninToken string }
	if err = json.NewDecoder(rsp.Body).Decode(&out); err != nil {
		return err
	}
	if out.SigninToken == "" {
		return errors.New("no SigninToken in AWS response")
	}

	q = make(url.Values)
	q.Set("Action", "login")
	q.Set("Destination", consURL)
	q.Set("SigninToken", out.SigninToken)
	req.URL.RawQuery = q.Encode()

	// TODO: There has to be a better way to do this
	browser.OpenURL(logoutURL)
	time.Sleep(1 * time.Second)
	return browser.OpenURL(req.URL.String())
}
