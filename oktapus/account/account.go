package account

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/region"
	"github.com/aws/aws-sdk-go-v2/aws"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ErrNoOrg indicates that the current account is not part of an organization.
var ErrNoOrg = errors.New("account: not in organization")

// ErrNotMaster indicates that the current account is not allowed to make
// certain API calls because it is not the organization master.
var ErrNotMaster = errors.New("account: not organization master")

// Info contains account information.
type Info struct {
	ID         string
	ARN        arn.ARN
	Name       string
	Alias      string
	Email      string
	Status     orgs.AccountStatus
	JoinMethod orgs.AccountJoinedMethod
	JoinTime   time.Time
}

// Set updates account information.
func (ac *Info) Set(src *orgs.Account) {
	*ac = Info{
		ID:         aws.StringValue(src.Id),
		ARN:        arn.Value(src.Arn),
		Name:       aws.StringValue(src.Name),
		Alias:      ac.Alias,
		Email:      aws.StringValue(src.Email),
		Status:     src.Status,
		JoinMethod: src.JoinedMethod,
		JoinTime:   aws.TimeValue(src.JoinedTimestamp),
	}
}

// DisplayName returns the account alias, if set, or its name.
func (ac *Info) DisplayName() string {
	if ac.Alias != "" {
		return ac.Alias
	}
	return ac.Name
}

// Org contains organization information.
type Org struct {
	ARN         arn.ARN
	FeatureSet  orgs.OrganizationFeatureSet
	ID          string
	MasterARN   arn.ARN
	MasterEmail string
	MasterID    string
}

// Set updates organization information.
func (o *Org) Set(src *orgs.Organization) {
	*o = Org{
		ARN:         arn.Value(src.Arn),
		FeatureSet:  src.FeatureSet,
		ID:          aws.StringValue(src.Id),
		MasterARN:   arn.Value(src.MasterAccountArn),
		MasterEmail: aws.StringValue(src.MasterAccountEmail),
		MasterID:    aws.StringValue(src.MasterAccountId),
	}
}

// Directory provides account information using AWS Organizations API and/or an
// alias file.
type Directory struct {
	client   *orgs.Organizations
	part     string
	org      Org
	aliases  map[string]string
	accounts map[string]*Info
}

// NewDirectory returns a new account directory. Refresh must be called to
// update organization info.
func NewDirectory(cfg *aws.Config) *Directory {
	return &Directory{
		client: orgs.New(*cfg),
		part:   region.Partition(cfg.Region),
	}
}

// Refresh updates account information.
func (d *Directory) Refresh() error {
	if region.Subset(d.part, orgs.ServiceName) == nil {
		return ErrNoOrg
	}
	var currentAccount string
	err := fast.Call(
		func() error {
			out, err := d.client.DescribeOrganizationRequest(nil).Send()
			if err == nil {
				d.org.Set(out.Organization)
			} else if d.org = (Org{}); awsx.IsCode(err, orgs.ErrCodeAWSOrganizationsNotInUseException) {
				err = ErrNoOrg
			}
			return err
		},
		func() error {
			c := sts.New(d.client.Config)
			out, err := c.GetCallerIdentityRequest(nil).Send()
			if err == nil {
				currentAccount = aws.StringValue(out.Account)
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	if currentAccount != d.org.MasterID {
		return ErrNotMaster
	}
	acs := make(map[string]*Info, len(d.accounts))
	r := d.client.ListAccountsRequest(nil)
	p := r.Paginate()
	for p.Next() {
		out := p.CurrentPage()
		for i := range out.Accounts {
			src := &out.Accounts[i]
			ac := d.accounts[aws.StringValue(src.Id)]
			if ac == nil {
				ac = new(Info)
			}
			ac.Set(src)
			acs[ac.ID] = ac
		}
	}
	if err = p.Err(); err == nil {
		d.accounts = acs
		d.applyAliases()
	}
	return nil
}

// Partition returns the AWS partition of directory d.
func (d *Directory) Partition() string { return d.part }

// Org returns organization information. The zero value is returned if the
// current account is not part of an organization.
func (d *Directory) Org() Org { return d.org }

// LoadAliases updates account information from an alias file.
func (d *Directory) LoadAliases(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	part := []byte(d.part)
	s, ln := bufio.NewScanner(f), 0
	aliases := make(map[string]string)
	for s.Scan() {
		f := bytes.Fields(s.Bytes())
		if ln++; len(f) != 3 {
			if len(f) == 0 || f[0][0] == '#' {
				continue
			}
			return fmt.Errorf("account: invalid record at %s:%d", file, ln)
		}
		if !bytes.Equal(f[0], part) {
			continue
		}
		account, alias := string(f[1]), string(f[2])
		if !awsx.IsAccountID(account) {
			return fmt.Errorf("account: invalid account id at %s:%d", file, ln)
		}
		if aliases[account] != "" {
			return fmt.Errorf("account: duplicate account id %s:%d", file, ln)
		}
		if alias == "" {
			return fmt.Errorf("account: invalid account alias at %s:%d", file, ln)
		}
		aliases[account] = alias
	}
	if err = s.Err(); err == nil {
		d.aliases = aliases
		d.applyAliases()
	}
	return err
}

// Accounts returns all known accounts sorted by ID.
func (d *Directory) Accounts() []*Info {
	acs := make([]*Info, 0, len(d.accounts))
	for _, ac := range d.accounts {
		acs = append(acs, ac)
	}
	sort.Slice(acs, func(i, j int) bool { return acs[i].ID < acs[j].ID })
	return acs
}

// applyAliases updates Info.Alias fields using current account aliases map.
func (d *Directory) applyAliases() {
	if d.accounts == nil && len(d.aliases) > 0 {
		d.accounts = make(map[string]*Info, len(d.aliases))
	}
	for id, alias := range d.aliases {
		ac := d.accounts[id]
		if ac == nil {
			ac = &Info{ID: id}
			d.accounts[id] = ac
		}
		ac.Alias = alias
	}
}
