package cmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"

	"github.com/LuminalHQ/oktapus/internal"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/pkg/browser"
)

const (
	fedURL  = "https://signin.aws.amazon.com/federation"
	consURL = "https://console.aws.amazon.com/"
)

func init() {
	register(&Console{command: command{
		name:    []string{"console", "cons"},
		summary: "Open AWS management console",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
	}})
}

type Console struct {
	command
	switchRole bool // TODO: Implement
}

func (cmd *Console) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.switchRole, "sr", false,
		`Open "Switch Role" page to avoid logging out`)
}

func (cmd *Console) Run(ctx *Ctx, args []string) error {
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	switch len(match) {
	case 0:
		log.F("Account not found")
	case 1:
	default:
		log.F("Multiple matching accounts found")
	}
	v, err := c.Creds(match[0].ID).Get()
	if err == nil {
		err = cmd.Open(v)
	}
	return err
}

// Open launches AWS management console using the process described here:
// https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_providers_enable-console-custom-url.html
func (*Console) Open(cr credentials.Value) error {
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

	return browser.OpenURL(req.URL.String())
}
