package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/okta"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"golang.org/x/crypto/ssh/terminal"
)

var log = internal.Log

const (
	oktaState = "okta"
	awsState  = "aws"
)

// Ctx provides global configuration information.
type Ctx struct {
	State *internal.State

	OktaHost       string
	OktaUser       string
	OktaAWSAppLink string
	AWSRoleARN     string

	okta *okta.Client
	aws  *awsgw.Client
}

// NewCtx populates a new context from the environment variables.
func NewCtx() *Ctx {
	// Using same env vars as github.com/oktadeveloper/okta-aws-cli-assume-role
	ctx := &Ctx{
		State:          internal.NewState(stateFile()),
		OktaHost:       os.Getenv("OKTA_ORG"),
		OktaUser:       os.Getenv("OKTA_USERNAME"),
		OktaAWSAppLink: os.Getenv("OKTA_AWS_APP_URL"),
		AWSRoleARN:     os.Getenv("OKTA_AWS_ROLE_TO_ASSUME"),
	}
	return ctx
}

// UseOkta returns true if Okta is used for authentication.
func (ctx *Ctx) UseOkta() bool {
	return ctx.OktaHost != ""
}

// Okta returns a client for Okta.
func (ctx *Ctx) Okta() *okta.Client {
	if ctx.okta != nil {
		return ctx.okta
	}
	if ctx.OktaHost == "" {
		log.F("Okta host not configured")
	}
	ctx.okta = okta.NewClient(ctx.OktaHost)
	if b := ctx.State.Get(oktaState); len(b) > 0 {
		if err := ctx.okta.GobDecode(b); err != nil {
			log.E("Failed to decode Okta client state: %v", err)
		}
	}
	if ctx.okta.Authenticated() {
		err := ctx.okta.RefreshSession()
		if err == nil {
			ctx.Save()
			return ctx.okta
		}
		log.W("Failed to refresh Okta session: %v", err)
	}
	if err := ctx.okta.Authenticate(termAuthn{ctx}); err != nil {
		log.F("Okta authentication failed: %v", err)
	}
	ctx.Save()
	return ctx.okta
}

// AWSAuth returns AWS authentication information from Okta.
func (ctx *Ctx) AWSAuth() *okta.AWSAuth {
	c := ctx.Okta()
	if ctx.OktaAWSAppLink == "" {
		apps, err := c.AppLinks()
		if err != nil {
			log.F("Failed to get Okta app links: %v", err)
		}
		var app *okta.AppLink
		multiple := false
		for _, a := range apps {
			if a.AppName == "amazon_aws" {
				if app == nil {
					app = a
				} else {
					multiple = true
				}
			}
		}
		if multiple {
			log.W("Multiple AWS apps found in Okta, using %q", app.Label)
		} else if app == nil {
			log.F("AWS app not found in Okta")
		}
		ctx.OktaAWSAppLink = app.LinkURL
	}
	auth, err := c.OpenAWS(ctx.OktaAWSAppLink, ctx.AWSRoleARN)
	if err != nil {
		log.F("Failed to get AWS SAML assertion from Okta: %v", err)
	}
	return auth
}

func (ctx *Ctx) AWS() *awsgw.Client {
	if ctx.aws != nil {
		return ctx.aws
	}
	// TODO: Figure out what to do with regions
	var cfg aws.Config
	if ctx.UseOkta() {
		// With Okta, all credentials must be explicit
		cfg.Credentials = credentials.NewCredentials(&credentials.ErrorProvider{
			Err:          errors.New("missing credentials"),
			ProviderName: "ErrorProvider",
		})
	}
	sess, err := session.NewSession(&cfg)
	if err != nil {
		log.F("Failed to create AWS session: %v", err)
	}
	ctx.aws = awsgw.NewClient(sess)
	if b := ctx.State.Get(awsState); len(b) > 0 {
		if err := ctx.aws.GobDecode(b); err != nil {
			log.E("Failed to decode AWS client state: %v", err)
		}
	}
	if ctx.aws.MasterCreds == nil && ctx.UseOkta() {
		auth := ctx.AWSAuth()
		role := auth.Roles[0]
		if len(auth.Roles) > 1 {
			log.W("Multiple AWS roles available, using %q", role.Role)
		}
		cfg.Credentials = credentials.AnonymousCredentials
		anonSTS := sts.New(sess, &cfg)
		ctx.aws.MasterCreds = auth.GetCreds(anonSTS.AssumeRoleWithSAML, role)
	}
	if err = ctx.aws.Connect(); err != nil {
		log.F("AWS connection failed: %v", err)
	}
	return ctx.aws
}

// Save writes context state to the state file.
func (ctx *Ctx) Save() {
	if ctx.okta != nil {
		if b, err := ctx.okta.GobEncode(); err == nil {
			if len(b) > 0 {
				ctx.State.Set(oktaState, b)
			}
		} else {
			log.E("Failed to encode Okta client state: %v", err)
		}
	}
	if ctx.aws != nil {
		if b, err := ctx.aws.GobEncode(); err == nil {
			if len(b) > 0 {
				ctx.State.Set(awsState, b)
			}
		} else {
			log.E("Failed to encode AWS client state: %v", err)
		}
	}
	if ctx.State != nil && ctx.State.Dirty() {
		ctx.State.Update()
		ctx.State.Save()
	}
}

// termAuthn uses the terminal for Okta authentication.
type termAuthn struct{ *Ctx }

// Username returns the username for Okta.
func (ctx termAuthn) Username() (string, error) {
	for ctx.OktaUser == "" {
		fmt.Fprintln(os.Stderr, "Okta username: ")
		if _, err := fmt.Scanln(&ctx.OktaUser); err != nil {
			return "", err
		}
		ctx.OktaUser = strings.TrimSpace(ctx.OktaUser)
	}
	return ctx.OktaUser, nil
}

// Password returns the user's Okta password.
func (ctx termAuthn) Password() (string, error) {
	defer fmt.Println()
	fmt.Fprintf(os.Stderr, "Okta password for %q: ", ctx.OktaUser)
	if terminal.IsTerminal(syscall.Stdin) {
		pw, err := terminal.ReadPassword(syscall.Stdin)
		return string(pw), err
	}
	fmt.Print("<WARNING! PASSWORD WILL ECHO!> ")
	var pw string
	_, err := fmt.Scanln(&pw)
	return pw, err
}

// SelectFactor picks the additional factor to use for authentication.
func (ctx termAuthn) SelectFactor(all []*okta.Factor) (*okta.Factor, error) {
	fmt.Fprintln(os.Stderr, "Multi-factor authentication required.")
	if len(all) == 1 {
		return all[0], nil
	}
	for {
		fmt.Fprintln(os.Stderr, "")
		for i, f := range all {
			fmt.Fprintf(os.Stderr, "[%d] %s\n", i+1, f.Info().Name)
		}
		fmt.Fprint(os.Stderr, "Which factor do you want to use? ")
		var n int
		// TODO: Handle number-specific errors
		if fmt.Scanln(&n); 1 <= n && n <= len(all) {
			return all[n-1], nil
		}
		fmt.Fprintln(os.Stderr, "Invalid choice, please try again.")
	}
}

// Challenge asks the user for factor verification.
func (ctx termAuthn) Challenge(f *okta.Factor) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", f.Info().Prompt)
	var rsp string
	_, err := fmt.Scanln(&rsp)
	return rsp, err
}

// stateFile returns the path to the state file that is used to preserve program
// state between invocations.
func stateFile() string {
	if u, err := user.Current(); err == nil {
		dir := filepath.Join(u.HomeDir, ".cache")
		if err := os.Mkdir(dir, 0700); err == nil || os.IsExist(err) {
			return filepath.Join(dir, "."+internal.AppName)
		}
		log.W("Failed to create ~/.cache/: %v", err)
	} else {
		log.W("Failed to get user information: %v", err)
	}
	return ""
}
