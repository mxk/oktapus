package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"runtime"
	"strings"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"master-setup"},
		Summary: "Configure master account for oktapus access",
		Usage:   "[options]",
		MaxArgs: -1,
		Hidden:  true,
		New:     func() op.Cmd { return &masterSetup{Name: "master-setup"} },
	})
}

type masterSetup struct {
	Name
	Exec bool
	CLI  bool
}

func (cmd *masterSetup) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Configure master account policies and roles for oktapus access.

		Most of the 'organizations' API calls can only be issued from the master
		account. The ones most relevant to oktapus are CreateAccount and
		ListAccounts. This command creates two new IAM policies and one new role
		in the master account that simplify oktapus provisioning.

		Two new policies are created:

		  OktapusGatewayAccess contains the minimum necessary privileges to use
		  the master account as oktapus gateway. This policy should be applied
		  to IAM users or a SAML-federated role in the master account.

		  OktapusCreateAccountAccess allows creating new accounts in the
		  organization. It can be applied in addition to OktapusGatewayAccess
		  for those users/roles who should be able to create accounts.

		One new role is created:

		  OktapusOrganizationsProxy allows any other account in the organization
		  to call ListAccounts. This API call can only be issued from the master
		  account, so when oktapus is using a non-master gateway, it assumes
		  this role to get the list of all accounts in the organization. The
		  role requires an External ID, which is derived from information
		  returned by the DescribeOrganization API.

		Without the -exec option, a dry-run is performed that shows all steps of
		the setup process. It outputs role and policy names, descriptions, and
		all JSON policy documents, making it possible to perform the entire
		setup procedure manually.
	`)
}

func (cmd *masterSetup) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.CLI, "cli", false,
		"Write AWS CLI commands to stdout without executing them")
	fs.BoolVar(&cmd.Exec, "exec", false,
		"Execute operation (dry-run otherwise)")
}

func (cmd *masterSetup) Run(ctx *op.Ctx, args []string) error {
	// Verify that current account is master
	gw := ctx.Gateway()
	org := gw.Org()
	log.I("Master account is: %s", org.MasterID)
	log.I("Authenticated as: %s", gw.Ident().UserARN)
	if !gw.IsMaster() {
		ignore := ""
		if !cmd.Exec {
			ignore = " (ignored for dry-run)"
		}
		log.E("Current account is not organization master%s", ignore)
		if cmd.Exec {
			return errors.New("unable to continue")
		}
	}

	// Create policies
	var ic iamiface.IAMAPI
	if cmd.CLI {
		if cmd.Exec {
			return errors.New("-cli and -exec are mutually exclusive")
		}
		ic = newCLIWriter(args...)
	} else if cmd.Exec {
		var cfg aws.Config
		if gw.Creds != nil {
			cfg.Credentials = gw.Creds.Creds()
		}
		ic = iam.New(gw, &cfg)
	}
	err := createPolicy(ic, op.IAMPath, gatewayAccessName, gatewayAccessDesc, &gatewayAccess)
	if err != nil {
		return err
	}
	err = createPolicy(ic, op.IAMPath, createAccountAccessName, createAccountAccessDesc, &createAccountAccess)
	if err != nil {
		return err
	}

	// Create proxy role
	proxyAssumeRole.Statement[0].
		Condition["StringEquals"]["sts:ExternalId"][0] = awsx.ProxyExternalID(&org)
	path, name := gw.MasterRole.Path(), gw.MasterRole.Name()
	err = createRole(ic, path, name, proxyRoleDesc, &proxyAssumeRole)
	if err != nil {
		return err
	}
	err = createInlinePolicy(ic, name, proxyAccessName, &proxyAccess)
	if err != nil || !cmd.Exec {
		return err
	}

	log.I("Account configured")
	log.I("Create new IAM users/roles with the %s policy attached to use the master account as gateway",
		gatewayAccessName)
	log.I("To allow account creation, also attach the %s policy",
		createAccountAccessName)
	return nil
}

const (
	gatewayAccessName = "OktapusGatewayAccess"
	gatewayAccessDesc = "Provides minimum privileges for using this account as a gateway."

	createAccountAccessName = "OktapusCreateAccountAccess"
	createAccountAccessDesc = "Allows creating new accounts in the organization."

	proxyAccessName = "ListAccountsAccess"
	proxyRoleDesc   = "Allows oktapus to list accounts in the organization."
)

var (
	gatewayAccess = op.Policy{Statement: []*op.Statement{{
		Effect: "Allow",
		Action: op.PolicyMultiVal{
			"organizations:DescribeOrganization",
			"organizations:ListAccounts",
		},
		Resource: op.PolicyMultiVal{"*"},
	}, {
		Effect:   "Allow",
		Action:   op.PolicyMultiVal{"sts:AssumeRole"},
		Resource: op.PolicyMultiVal{"arn:aws:iam::*:role/oktapus/${aws:username}"},
	}, {
		Effect:   "Allow",
		Action:   op.PolicyMultiVal{"sts:AssumeRole"},
		Resource: op.PolicyMultiVal{"arn:aws:iam::*:role/oktapus/${saml:sub}"},
	}}}

	createAccountAccess = op.Policy{Statement: []*op.Statement{{
		Effect: "Allow",
		Action: op.PolicyMultiVal{
			"organizations:CreateAccount",
			"organizations:DescribeAccount",
			"organizations:DescribeCreateAccountStatus",
			"organizations:ListCreateAccountStatus",
		},
		Resource: op.PolicyMultiVal{"*"},
	}, {
		Effect:   "Allow",
		Action:   op.PolicyMultiVal{"sts:AssumeRole"},
		Resource: op.PolicyMultiVal{"arn:aws:iam::*:role/" + accountSetupRole},
	}}}

	proxyAssumeRole = op.Policy{Statement: []*op.Statement{{
		Effect:    "Allow",
		Principal: op.NewAWSPrincipal("*"),
		Action:    op.PolicyMultiVal{"sts:AssumeRole"},
		Condition: op.ConditionMap{
			"StringEquals": op.Conditions{
				"sts:ExternalId": op.PolicyMultiVal{""},
			},
		},
	}}}

	proxyAccess = op.Policy{Statement: []*op.Statement{{
		Effect:   "Allow",
		Action:   op.PolicyMultiVal{"organizations:ListAccounts"},
		Resource: op.PolicyMultiVal{"*"},
	}}}
)

func createPolicy(c iamiface.IAMAPI, path, name, desc string, pol *op.Policy) error {
	in := iam.CreatePolicyInput{
		Description:    aws.String(desc),
		Path:           aws.String(path),
		PolicyDocument: pol.Doc(),
		PolicyName:     aws.String(name),
	}
	log.I("Create policy: %s%s (%s)\n%s", *in.Path, *in.PolicyName,
		*in.Description, indentDoc(in.PolicyDocument))
	if c == nil {
		return nil
	}
	_, err := c.CreatePolicy(&in)
	return ignoreExists("Policy", err)
}

func createRole(c iamiface.IAMAPI, path, name, desc string, assumeRolePol *op.Policy) error {
	in := iam.CreateRoleInput{
		AssumeRolePolicyDocument: assumeRolePol.Doc(),
		Description:              aws.String(desc),
		Path:                     aws.String(path),
		RoleName:                 aws.String(name),
	}
	log.I("Create role: %s%s (%s)\n%s", *in.Path, *in.RoleName, *in.Description,
		indentDoc(in.AssumeRolePolicyDocument))
	if c == nil {
		return nil
	}
	_, err := c.CreateRole(&in)
	return ignoreExists("Role", err)
}

func createInlinePolicy(c iamiface.IAMAPI, role, name string, pol *op.Policy) error {
	in := iam.PutRolePolicyInput{
		PolicyDocument: pol.Doc(),
		PolicyName:     aws.String(name),
		RoleName:       aws.String(role),
	}
	log.I("Create inline policy for %s: %s\n%s", *in.RoleName, *in.PolicyName,
		indentDoc(in.PolicyDocument))
	if c == nil {
		return nil
	}
	_, err := c.PutRolePolicy(&in)
	return ignoreExists("Policy", err)
}

func indentDoc(doc *string) []byte {
	var buf bytes.Buffer
	err := json.Indent(&buf, []byte(*doc), "", "  ")
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func ignoreExists(what string, err error) error {
	if awsx.IsCode(err, iam.ErrCodeEntityAlreadyExistsException) {
		log.W("%s already exists: %s", what, err.(awserr.Error).Message())
		err = nil
	}
	return err
}

type cliWriter struct {
	iamiface.IAMAPI
	cmd string
}

func newCLIWriter(opts ...string) *cliWriter {
	cmd := "aws"
	if len(opts) > 0 {
		cmd += " " + strings.Join(opts, " ")
	}
	return &cliWriter{cmd: cmd}
}

func (w *cliWriter) CreatePolicy(in *iam.CreatePolicyInput) (*iam.CreatePolicyOutput, error) {
	return nil, w.write("iam", "create-policy", in)
}

func (w *cliWriter) CreateRole(in *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	return nil, w.write("iam", "create-role", in)
}

func (w *cliWriter) PutRolePolicy(in *iam.PutRolePolicyInput) (*iam.PutRolePolicyOutput, error) {
	return nil, w.write("iam", "put-role-policy", in)
}

func (w *cliWriter) write(service, api string, in interface{}) error {
	if runtime.GOOS == "windows" {
		// On Windows arguments must use double quotes with non-trivial escape
		// rules, so leaving this for another day.
		panic("windows not supported for generating cli commands")
	}
	s, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %s %s --cli-input-json '%s'\n",
		w.cmd, service, api, bytes.Replace(s, []byte(`'`), []byte(`\'`), -1))
	return nil
}
