package op

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// CtlRole is the IAM role that stores account control information in its
// description.
const CtlRole = "OktapusAccountControl"

// ErrNoCtl indicates missing account control information.
var ErrNoCtl = errors.New("account control not initialized")

// errCtlUpdate indicates that new account control information was not saved.
var errCtlUpdate = errors.New("account control information update interrupted")

// Ctl contains account control information.
type Ctl struct {
	Owner string `json:"owner,omitempty"`
	Desc  string `json:"desc,omitempty"`
	Tags  Tags   `json:"tags,omitempty"`
}

// Init creates account control information in an uncontrolled account.
func (ctl *Ctl) Init(c iamx.Client) error {
	return ctl.exec(c, func(c iamx.Client, b64 string) (*iam.Role, error) {
		in := iam.CreateRoleInput{
			AssumeRolePolicyDocument: iamx.AssumeRolePolicy("").Doc(),
			Description:              aws.String(b64),
			Path:                     aws.String(IAMPath),
			RoleName:                 aws.String(CtlRole),
		}
		out, err := c.CreateRoleRequest(&in).Send()
		if err != nil {
			return nil, err
		}
		if out.Role.Description == nil {
			// Probably a bug, but CreateRole does not return the description
			out.Role.Description = in.Description
		}
		return out.Role, nil
	})
}

// Get retrieves current account control information.
func (ctl *Ctl) Get(c iamx.Client) error {
	in := iam.GetRoleInput{RoleName: aws.String(CtlRole)}
	out, err := c.GetRoleRequest(&in).Send()
	if err == nil {
		return ctl.decode(out.Role.Description)
	}
	if *ctl = (Ctl{}); awsx.IsCode(err, iam.ErrCodeNoSuchEntityException) {
		err = ErrNoCtl
	}
	return err
}

// Set stores account control information.
func (ctl *Ctl) Set(c iamx.Client) error {
	return ctl.exec(c, func(c iamx.Client, b64 string) (*iam.Role, error) {
		in := iam.UpdateRoleDescriptionInput{
			Description: aws.String(b64),
			RoleName:    aws.String(CtlRole),
		}
		out, err := c.UpdateRoleDescriptionRequest(&in).Send()
		if err == nil {
			return out.Role, nil
		}
		if awsx.IsCode(err, iam.ErrCodeNoSuchEntityException) {
			err = ErrNoCtl
		}
		return nil, err
	})
}

// eq returns true if ctl == other.
func (ctl *Ctl) eq(other *Ctl) bool {
	return ctl == other || (ctl != nil && other != nil &&
		ctl.Owner == other.Owner && ctl.Desc == other.Desc &&
		internal.StringsEq(ctl.Tags, other.Tags))
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
	b64, err := ctl.encode()
	if err != nil {
		return err
	}
	r, err := fn(c, b64)
	if err == nil && aws.StringValue(r.Description) != b64 {
		err = errCtlUpdate
	}
	return err
}

const ctlVer = "1#"

// encode encodes account control information into a base64 string.
func (ctl *Ctl) encode() (string, error) {
	sort.Strings(ctl.Tags)
	b, err := json.Marshal(ctl)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding
	b64 := make([]byte, len(ctlVer)+enc.EncodedLen(len(b)))
	enc.Encode(b64[copy(b64, ctlVer):], b)
	return string(b64), nil
}

// decode decodes account control information from a base64 string.
func (ctl *Ctl) decode(s *string) error {
	if *ctl = (Ctl{}); s == nil || *s == "" {
		return nil
	}
	b64, ver := *s, 0
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
		sort.Strings(ctl.Tags)
	}
	return err
}
