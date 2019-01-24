Oktapus
=======

Oktapus is a command-line tool that manages and provides access to multiple AWS
accounts. One account is used as the gateway. The client authenticates to the
gateway via IAM user credentials or STS role-based access (e.g. SAML assertion
provided by Okta). After authentication, AWS policy associated with the gateway
user/role determines which other accounts the client is allowed to access.

Each account may contain a special IAM role called `OktapusAccountControl`,
which is used to store account metadata consisting of the Owner, Description,
and Tags. This metadata is represented as a JSON object, encoded in base 64, and
stored in the role description.

If the gateway account is an AWS Organizations master, and the policy allows the
client to call `organizations:CreateAccount` API (and a few related APIs), then
the client is also able to create new accounts in the organization. Accounts
cannot be deleted programmatically (see [Limitations](#limitations)).

Setup
-----

Okta-based authentication is not covered by these instructions. Use one AWS
account as a gateway by following these steps:

1. Use your email address as the name of a new IAM user in your AWS account.
   Only programmatic access is required.
2. Run `aws configure [--profile <name>]` to add the user credentials to your
   aws cli config file. If you use a separate profile, set `OKTAPUS_AWS_PROFILE`
   environment variable to the profile name.
3. Send your user ARN to someone who already has access to other accounts.
   * The following command is used to give account access:
     ```
     oktapus authz <account-spec> <principal> ...
     ```
   * For example, this command creates a new role called `newuser@example.com`
     in all accounts with the `test` tag. The role can be assumed by the user
     `newuser@example.com` from account 123456789012:
     ```
     oktapus authz test arn:aws:iam::123456789012:user/newuser@example.com
     ```
4. Once your new user is authorized, run `oktapus ls` to confirm access. If you
   ran any command before, the access errors may still be cached by the daemon.
   Run `oktapus kill-daemon` to wipe that cache.
5. Read `oktapus help account-spec` to understand how accounts are specified on
   the command-line. This argument is expected by most sub-commands.

Basic use
---------

These are simple examples of the most common oktapus operations. Run `oktapus`
to see a list of all available commands. Run `oktapus help <command>` to see
detailed command help information.

```sh
# List free accounts
oktapus ls '!owner'

# Open AWS console for an account ID
oktapus cons 123456789012

# Allocate 2 accounts
oktapus alloc 2

# Allow another user to access your accounts
oktapus authz owner=me someone@example.com

# Get long-term credentials for a temporary IAM user
oktapus creds -tmp -user mytmpuser owner=me

# Free accounts allocated to you
oktapus free
```

Design
------

### Authentication

In the absence of any other configuration, oktapus uses the standard AWS CLI
config files and environment variables to authenticate to the gateway account.

To use Okta authentication, set `OKTA_ORG` environment variable to your Okta
domain name (e.g. `<orgname>.okta.com`).
[Additional variables](https://github.com/oktadeveloper/okta-aws-cli-assume-role#configuring-the-application)
may be used to specify your username, the AWS app URL within Okta, and the AWS
role to assume (if the SAML assertion contains more than one). `OKTA_PASSWORD`
is not supported by oktapus.

For the initial Okta authentication, the client prompts for the username (if not
set in `OKTA_USERNAME`), password, and MFA, if required. These credentials are
exchanged for a session cookie, which is stored in the oktapus cache file
(`~/.cache/.oktapus`). To get AWS credentials, oktapus requests a SAML assertion
from Okta, which is a signed XML document that tells AWS who the subject is and
which role(s) they are allowed to assume via `sts:AssumeRoleWithSAML` API call.
Oktapus exchanges the SAML assertion for temporary security credentials in the
gateway account.

### Authorization

Once in the gateway account, the policy associated with the authenticated IAM
user or role determines which other accounts the client is allowed to access.
There are multiple ways to implement this control. One of them is to use policy
variables that allow the client to assume only specially named roles in other
accounts. The minimal policy to implement this is below:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "id0",
            "Effect": "Allow",
            "Action": "sts:AssumeRole",
            "Resource": ["arn:aws:iam::*:role/${aws:username}"]
        },
        {
            "Sid": "id1",
            "Effect": "Allow",
            "Action": "sts:AssumeRole",
            "Resource": ["arn:aws:iam::*:role/${saml:sub}"]
        }
    ]
}
```

Both statements in this policy do the same thing, but the first one only applies
to IAM users (those not using Okta authentication), while the second one only
applies to SAML-assumed IAM roles. This policy allows the client to assume a
role in any other account, but only if that account contains a role that matches
the client's user name. In other words, if the client is authenticated into the
gateway account as an IAM user `someone@example.com`, only accounts that have a
role named `someone@example.com` will be accessible to that client. The policy
associated with that role determines what the client is allowed to do in the
other account.

For Okta authentication, `${saml:sub}`, which is the subject ID from the SAML
assertion, determines the role name in other accounts. By default, Okta sets the
subject ID to the user's email address, but this is configurable in Okta's Admin
settings.

Because the client has no control over the IAM user name or Okta's signed SAML
assertion, the client is not able to gain access to any accounts where the
appropriately named role does not already exist. This presents a problem when
creating new AWS accounts. By default, each new account is provisioned with a
role called `OrganizationAccountAccessRole`. Since this role name does not meet
the resource restrictions in the policy above, the client would be immediately
locked out of any newly created account. To solve this problem, oktapus
specifies an explicit initial role for new accounts that the current user is
allowed to assume, and then creates `OrganizationAccountAccessRole` separately.

### Account ownership

Account control metadata stored in the `OktapusAccountControl` IAM role defines
an owner field that behaves like a per-account mutex. The value of the field has
no special meaning. All that matters for account allocation is whether the field
is empty or not.

Oktapus uses the following algorithm when asked to allocate N accounts (see
`oktapus help alloc` for more information):

1. All known accounts are filtered according to the `account-spec` argument.
2. All free accounts (those with an empty owner) are shuffled to randomize their
   order.
3. For each free account:
   1. Get current account control information.
   2. Merge local changes to avoid overwriting modifications made by other
      clients.
   3. If the owner field is still empty, set it to the current user, and update
      the role description. If the field was not empty, account is skipped.
   4. Wait for a verification delay (currently set at 10 seconds).
   5. Get current account control information and confirm that the owner is
      still set to the current user. If so, the account is allocated.
   6. Stop allocation once N accounts have been allocated or there are no free
      accounts remaining.
4. If N accounts have been allocated, return their credentials to the user.
   Otherwise, free any allocated accounts and return an error.

The situation that this algorithm was designed to avoid is one where two
separate clients try to allocate the same account at the same time, and both end
up thinking that their allocation succeeded. The verification delay allows
simultaneous updates from multiple clients to propagate and resolve to one final
version. When each client retrieves account metadata for the second time, each
one should only see the "winning" version, and only one client will consider
itself the account owner. Theoretically, it is still possible to end up in a
split ownership situation if the update propagation takes an unusually long
time, but the chances of this happening are very low.

The verification delay was determined by running a stress test where 50
independent threads attempted to allocate the same account at the same time. A
total of 1,100 trials were performed. A trial passed if exactly one of the 50
threads considered themselves the account owner. The verification delay was
initialized to 0 and automatically incremented by 1 second after each failed
trial. With a delay of 7 seconds, 870 consecutive trials were executed without
any failures.

### Temporary IAM users/roles

The `creds` and `authz` commands are used to get account credentials and create
new IAM roles, respectively. By default, `creds` returns a set of temporary
credentials for the IAM role that was assumed via the gateway account. The
`authz` command is normally used to grant other users access to an account by
creating an appropriately named role that they are allowed to assume.

Both commands can also be used to create temporary IAM users and roles, which
are intended to be used only while the account is allocated. Temporary, in this
context, means that user/role is deleted automatically when the account is
freed. A temporary IAM user provides long-term credentials that do not expire
after one hour. A temporary IAM role allows cross-account access to accounts
other than the gateway or the organization master.

Temporary users and roles are identified by having `/oktapus/tmp/` set as their
path. When an account is freed, all IAM users/roles under this path are deleted.

Limitations
-----------

* AWS accounts cannot be deleted. An account can be closed, but only by the root
  user, which requires going through the email password reset procedure, and
  only via the dashboard (no API for this). Any account (closed or otherwise)
  can be removed from an organization and become a standalone account, but only
  after configuring support, billing, and contact info, and agreeing to the EULA
  (all of which must also be done by root):
  * https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/close-account.html
  * https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_accounts_remove.html
  * https://docs.aws.amazon.com/general/latest/gr/aws_tasks-that-require-root.html
  * https://aws.amazon.com/blogs/security/aws-organizations-now-supports-self-service-removal-of-accounts-from-an-organization/
* There is a limit on the number of accounts that can exist in an AWS
  organization. Increasing the limit requires contacting support:
  * https://docs.aws.amazon.com/organizations/latest/userguide/orgs_reference_limits.html
* The JSON object that contains account control information is limited to 750
  bytes (1000 bytes in base 64 encoding). This is more than enough to store the
  account owner, description, and tags.
