package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"syscall"

	"oktapus/awsgw"
	"oktapus/internal"
	"oktapus/okta"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"golang.org/x/crypto/ssh/terminal"
)

var log = internal.Log

const (
	oktaState = "okta"
	//awsState  = "aws"
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
	return ctx.oktaAuthn(ctx.okta)
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
		// With Okta, all credentials must be provided explicitly
		cfg.Credentials = credentials.NewCredentials(&credentials.ErrorProvider{
			Err:          errors.New("missing credentials"),
			ProviderName: "ErrorProvider",
		})
	}
	sess, err := session.NewSession(&cfg)
	if err != nil {
		log.F("Failed to create AWS session: %v", err)
	}
	if ctx.UseOkta() {
		// TODO: Use Okta as an on-demand credentials provider
		auth := ctx.AWSAuth()
		role := auth.Roles[0]
		if len(auth.Roles) > 1 {
			log.W("Multiple AWS roles available, using %q", role.Role)
		}
		cfg.Credentials = credentials.AnonymousCredentials
		anonSTS := sts.New(sess, &cfg)
		cfg.Credentials = auth.GetCreds(anonSTS.AssumeRoleWithSAML, role)
	}
	c, err := awsgw.NewClient(sess, cfg.Credentials)
	if err != nil {
		log.F("Failed to create AWS client: %v", err)
	}
	// TODO: Restore state
	ctx.aws = c
	return c
}

// Save writes context state to the state file.
func (ctx *Ctx) Save() {
	if ctx.okta != nil {
		if b, err := ctx.okta.GobEncode(); err == nil {
			ctx.State.Set(oktaState, b)
		} else {
			log.E("Failed to encode Okta client state: %v", err)
		}
	}
	if ctx.State != nil && ctx.State.Dirty() {
		ctx.State.Update()
		ctx.State.Save()
	}
}

// oktaAuthn establishes or resumes an Okta session.
func (ctx *Ctx) oktaAuthn(c *okta.Client) *okta.Client {
	if c.Authenticated() {
		err := c.RefreshSession()
		if err == nil {
			ctx.Save()
			return c
		}
		log.W("Failed to refresh Okta session: %v", err)
	}
	for ctx.OktaUser == "" {
		fmt.Println("Okta username: ")
		fmt.Scanln(&ctx.OktaUser)
	}
	fmt.Printf("Okta password for %q: ", ctx.OktaUser)
	pass, err := readPassword()
	fmt.Println()
	if err != nil {
		log.F("Failed to read password: %v", err)
	}
	if err := c.Authn(ctx.OktaUser, pass); err != nil {
		log.F("Okta authentication failed: %v", err)
	}
	ctx.Save()
	return c
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

// readPassword tries to read a password without echo.
func readPassword() (string, error) {
	if !terminal.IsTerminal(syscall.Stdin) {
		fmt.Print("<WARNING! PASSWORD WILL ECHO!> ")
		var pw string
		_, err := fmt.Scanln(&pw)
		return pw, err
	}
	pw, err := terminal.ReadPassword(syscall.Stdin)
	return string(pw), err
}
