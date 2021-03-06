package op

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/mxk/go-cloud/aws/awsx"
	"github.com/mxk/go-cloud/aws/iamx"
)

// CtlRole is the IAM role that stores account control information in its
// description.
const CtlRole = "OktapusAccountControl"

// Ctl contains account control information.
type Ctl struct {
	Owner string `json:"owner,omitempty"`
	Desc  string `json:"desc,omitempty"`
	Tags  Tags   `json:"tags,omitempty"`
}

// Init creates account control information in an uncontrolled account.
func (ctl *Ctl) Init(c iamx.Client) error {
	return ctl.exec(c, func(c iamx.Client, b64 string) (*iam.Role, error) {
		pol := iamx.AssumeRolePolicy(iamx.Deny, "*").Doc()
		in := iam.CreateRoleInput{
			AssumeRolePolicyDocument: pol,
			Description:              aws.String(b64),
			Path:                     aws.String(IAMPath),
			RoleName:                 aws.String(CtlRole),
		}
		out, err := c.CreateRoleRequest(&in).Send()
		if err != nil {
			return nil, err
		}
		if out.Role.Description == nil {
			// Probably an AWS bug; CreateRole does not return the description
			out.Role.Description = in.Description
		}
		return out.Role, nil
	})
}

// Load retrieves current account control information.
func (ctl *Ctl) Load(c iamx.Client) error {
	in := iam.GetRoleInput{RoleName: aws.String(CtlRole)}
	out, err := c.GetRoleRequest(&in).Send()
	if err == nil {
		return ctl.Decode(aws.StringValue(out.Role.Description))
	}
	if *ctl = (Ctl{}); awsx.ErrCode(err) == iam.ErrCodeNoSuchEntityException {
		err = ErrNoCtl
	}
	return err
}

// Store stores account control information.
func (ctl *Ctl) Store(c iamx.Client) error {
	return ctl.exec(c, func(c iamx.Client, b64 string) (*iam.Role, error) {
		in := iam.UpdateRoleDescriptionInput{
			Description: aws.String(b64),
			RoleName:    aws.String(CtlRole),
		}
		out, err := c.UpdateRoleDescriptionRequest(&in).Send()
		if err == nil {
			return out.Role, nil
		}
		if awsx.ErrCode(err) == iam.ErrCodeNoSuchEntityException {
			err = ErrNoCtl
		}
		return nil, err
	})
}

const ctlVer = "1#"

// Encode encodes account control information into a base64 string.
func (ctl *Ctl) Encode() (string, error) {
	ctl.Tags.Sort()
	b, err := json.Marshal(ctl)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding
	b64 := make([]byte, len(ctlVer)+enc.EncodedLen(len(b)))
	enc.Encode(b64[copy(b64, ctlVer):], b)
	return string(b64), nil
}

// Decode decodes account control information from a base64 string.
func (ctl *Ctl) Decode(b64 string) error {
	if *ctl = (Ctl{}); b64 == "" {
		return nil
	}
	ver := 0
	if i := strings.IndexByte(b64, '#'); i > 0 {
		if v, err := strconv.Atoi(b64[0:i]); err == nil {
			b64, ver = b64[i+1:], v
		}
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err == nil {
		if ver == 1 {
			if err = json.Unmarshal(b, ctl); err != nil {
				*ctl = Ctl{}
			}
		} else {
			err = fmt.Errorf("invalid account control version (%d)", ver)
		}
		ctl.Tags.Sort()
	}
	return err
}

// eq returns true if ctl == other.
func (ctl *Ctl) eq(other *Ctl) bool {
	return ctl == other || (ctl != nil && other != nil &&
		ctl.Owner == other.Owner && ctl.Desc == other.Desc &&
		ctl.Tags.eq(other.Tags))
}

// copy performs a deep copy of other to ctl.
func (ctl *Ctl) copy(other *Ctl) {
	if ctl != other {
		if ctl.Tags.alias(other.Tags) {
			panic("op: tag aliasing detected during copy")
		}
		tags := append(ctl.Tags[:0], other.Tags...)
		*ctl = *other
		ctl.Tags = tags
	}
}

// merge performs a 3-way merge of account control information changes.
func (ctl *Ctl) merge(cur, ref *Ctl) {
	if ctl.Tags.alias(cur.Tags) || ctl.Tags.alias(ref.Tags) ||
		cur.Tags.alias(ref.Tags) {
		panic("op: tag aliasing detected during merge")
	}
	if ctl.Owner == ref.Owner {
		ctl.Owner = cur.Owner
	}
	if ctl.Desc == ref.Desc {
		ctl.Desc = cur.Desc
	}
	set, clr := ctl.Tags.Diff(ref.Tags)
	ctl.Tags = append(ctl.Tags[:0], cur.Tags...)
	ctl.Tags.Apply(set, clr)
}

// exec executes init or set operations.
func (ctl *Ctl) exec(c iamx.Client, fn func(c iamx.Client, b64 string) (*iam.Role, error)) error {
	b64, err := ctl.Encode()
	if err != nil {
		return err
	}
	r, err := fn(c, b64)
	if err == nil && aws.StringValue(r.Description) != b64 {
		err = ErrCtlUpdate
	}
	return err
}
