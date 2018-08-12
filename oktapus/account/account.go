package account

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/region"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
)

var errNoOrg = awserr.New(orgs.ErrCodeAWSOrganizationsNotInUseException,
	"organizations api not supported in current partition", nil)

// IsID returns true if id is a valid AWS account ID.
func IsID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, c := range []byte(id) {
		if c-'0' > 9 {
			return false
		}
	}
	return true
}

// IsErrorNoOrg returns true if err indicates that Organizations API is not
// available for the current account.
func IsErrorNoOrg(err error) bool {
	e, ok := err.(awserr.Error)
	return ok && e.Code() == orgs.ErrCodeAWSOrganizationsNotInUseException
}

// LoadAliases loads account aliases from a file. The file should contain one
// alias per line in the format "<partition> <account-id> <alias>". Empty lines
// and lines beginning with '#' are ignored.
func LoadAliases(file, partition string) (map[string]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, ln := bufio.NewScanner(f), 0
	m := make(map[string]string)
	for s.Scan() {
		f := bytes.Fields(s.Bytes())
		if ln++; len(f) != 3 {
			if len(f) == 0 || f[0][0] == '#' {
				continue
			}
			return nil, fmt.Errorf("account: invalid alias record at %s:%d",
				file, ln)
		}
		if string(f[0]) != partition {
			continue
		}
		id, alias := string(f[1]), string(f[2])
		if !IsID(id) {
			return nil, fmt.Errorf("account: invalid account id at %s:%d",
				file, ln)
		}
		if alias == "" {
			return nil, fmt.Errorf("account: invalid account alias at %s:%d",
				file, ln)
		}
		m[id] = alias
	}
	if err = s.Err(); err != nil || len(m) == 0 {
		m = nil
	}
	return m, err
}

// Info contains account information.
type Info struct {
	arn.ARN
	ID         string
	Name       string
	Email      string
	Status     orgs.AccountStatus
	JoinMethod orgs.AccountJoinedMethod
	JoinTime   time.Time
}

// Set updates account information.
func (ac *Info) Set(src *orgs.Account) {
	ac.ARN = arn.Value(src.Arn)
	ac.ID = aws.StringValue(src.Id)
	ac.Name = aws.StringValue(src.Name)
	ac.Email = aws.StringValue(src.Email)
	ac.Status = src.Status
	ac.JoinMethod = src.JoinedMethod
	ac.JoinTime = aws.TimeValue(src.JoinedTimestamp)
}

// Org contains organization information.
type Org struct {
	arn.ARN
	ID          string
	FeatureSet  orgs.OrganizationFeatureSet
	Master      arn.ARN
	MasterEmail string
	MasterID    string
}

// Set updates organization information.
func (o *Org) Set(src *orgs.Organization) {
	o.ARN = arn.Value(src.Arn)
	o.ID = aws.StringValue(src.Id)
	o.FeatureSet = src.FeatureSet
	o.Master = arn.Value(src.MasterAccountArn)
	o.MasterEmail = aws.StringValue(src.MasterAccountEmail)
	o.MasterID = aws.StringValue(src.MasterAccountId)
}

// Directory retrieves account information from AWS Organizations API.
type Directory struct {
	Client   orgs.Organizations
	Org      Org
	Accounts map[string]*Info
}

// NewDirectory returns a new account directory.
func NewDirectory(cfg *aws.Config) *Directory {
	return &Directory{Client: *orgs.New(*cfg)}
}

// Init initializes organization information.
func (d *Directory) Init() error {
	if d.noOrg() {
		return errNoOrg
	}
	out, err := d.Client.DescribeOrganizationRequest(nil).Send()
	if err == nil {
		d.Org.Set(out.Organization)
	}
	return err
}

// Refresh updates account information.
func (d *Directory) Refresh() error {
	if d.noOrg() {
		return errNoOrg
	}
	m := make(map[string]*Info, len(d.Accounts))
	r := d.Client.ListAccountsRequest(nil)
	p := r.Paginate()
	for p.Next() {
		out := p.CurrentPage()
		buf := make([]Info, len(out.Accounts))
		for i := range out.Accounts {
			ac := &buf[i]
			ac.Set(&out.Accounts[i])
			m[ac.ID] = ac
		}
	}
	err := p.Err()
	if err == nil {
		d.Accounts = m
	}
	return err
}

// noOrg returns true if the AWS Organizations API is not available in the
// current partition.
func (d *Directory) noOrg() bool {
	p := region.Partition(d.Client.Config.Region)
	return region.Subset(p, orgs.ServiceName) == nil
}
