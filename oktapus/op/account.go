package op

import (
	"sort"

	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// Account is an account in an AWS organization.
type Account struct {
	*Ctl
	ID   string
	Name string
	Err  error

	// TODO: Add partition here for GovCloud support?

	iam iam.IAM
	ref Ctl
}

// NewAccount creates a new account with the given id and name.
func NewAccount(id, name string) *Account {
	return &Account{ID: id, Name: name}
}

// Init initializes the account IAM client.
func (ac *Account) Init(cfg *aws.Config, cp *creds.Provider) {
	ac.iam = *iam.New(*cfg)
	ac.iam.Credentials = cp
}

// IAM returns the account IAM client.
func (ac *Account) IAM() *iam.IAM {
	return &ac.iam
}

// CredsProvider returns the credentials provider for account ac.
func (ac *Account) CredsProvider() *creds.Provider {
	return ac.iam.Credentials.(*creds.Provider)
}

// Accounts is a group of accounts that can be operated on concurrently.
type Accounts []*Account

// Sort sorts accounts by name.
func (s Accounts) Sort() Accounts {
	// TODO: Natural number sorting
	sort.Sort(byName(s))
	return s
}

// Filter removes those accounts for which fn evaluates to false. This is done
// in-place without making a copy of the original slice.
func (s Accounts) Filter(fn func(ac *Account) bool) Accounts {
	var n, first, last int
	for i, ac := range s {
		if fn(ac) {
			n++
			if last = i; n == 1 {
				first = i
			}
		} else {
			s[i] = nil
		}
	}
	f := s[:0]
	if n > 0 {
		//noinspection GoAssignmentToReceiver
		if s = s[first : last+1]; n < len(s) {
			for _, ac := range s {
				if ac != nil {
					f = append(f, ac)
				}
			}
		} else if first > 0 {
			f = append(f, s...)
		} else {
			f = s
		}
	}
	return f
}

// RequireCtl ensures that all accounts have control information. Existing
// information is not refreshed.
func (s Accounts) RequireCtl() Accounts {
	var refresh Accounts
	for i, ac := range s {
		if ac.Ctl == nil && ac.Err == nil {
			if len(refresh) == 0 {
				refresh = make(Accounts, 0, len(s)-i)
			}
			refresh = append(refresh, ac)
		}
	}
	if len(refresh) > 0 {
		refresh.RefreshCtl()
	}
	return s
}

// RefreshCtl retrieves current control information for all accounts.
func (s Accounts) RefreshCtl() Accounts {
	return s.Apply(func(_ int, ac *Account) {
		if ac.Err = ac.ref.Get(ac.iam); ac.Err != nil {
			ac.Ctl = nil
		} else {
			if ac.Ctl == nil {
				ac.Ctl = new(Ctl)
			}
			ac.Ctl.copy(&ac.ref)
		}
	})
}

// Save updates control information for all accounts. When changing the owner,
// the caller must refresh account control information after a delay to confirm
// owner status.
func (s Accounts) Save() Accounts {
	return s.Apply(func(_ int, ac *Account) {
		if ac.Ctl == nil {
			if ac.Err == nil {
				ac.Err = ErrNoCtl
			}
			return
		}

		// Get current state and merge changes
		var cur Ctl
		if ac.Err = cur.Get(ac.iam); ac.Err != nil {
			return
		} else if ac.merge(&cur, &ac.ref); cur.eq(ac.Ctl) {
			ac.ref = cur
			return // Nothing to do
		}

		// To change the owner, current and reference states must match
		if cur.Owner != ac.Owner && cur.Owner != ac.ref.Owner {
			ac.Err, ac.ref = errCtlUpdate, cur
			return
		}

		// Update state
		if ac.Err = ac.Ctl.Set(ac.iam); ac.Err == nil {
			ac.ref.copy(ac.Ctl)
		}
	})
}

// Apply executes fn on each account concurrently.
func (s Accounts) Apply(fn func(i int, ac *Account)) Accounts {
	fast.ForEachIO(len(s), func(i int) error {
		fn(i, s[i])
		return nil
	})
	return s
}

// byName implements sort.Interface to sort accounts by name.
type byName Accounts

func (s byName) Len() int      { return len(s) }
func (s byName) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool {
	if s[i].Name == s[j].Name {
		return s[i].ID < s[j].ID
	}
	return s[i].Name < s[j].Name
}
