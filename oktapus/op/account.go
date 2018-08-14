package op

import (
	"sort"
	"strconv"
	"strings"

	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
)

// Account maintains control information and provides IAM access for one AWS
// account.
type Account struct {
	ID     string
	Name   string
	IAM    iamx.Client
	HasCtl bool // TODO: Replace with flags to also track access
	Ctl    Ctl
	Err    error

	ref Ctl
	key sortKey
}

// NewAccount creates a new account with the given id and name.
func NewAccount(id, name string) *Account {
	return &Account{ID: id, Name: name, key: natSortKey(name + "\x00" + id)}
}

// CredsProvider returns the credentials provider for account ac.
func (ac *Account) CredsProvider() *creds.Provider {
	return ac.IAM.Config.Credentials.(*creds.Provider)
}

// lostCtl resets account state to indicate missing control information.
func (ac *Account) lostCtl() {
	ac.HasCtl = false
	ac.Ctl = Ctl{}
	ac.ref = Ctl{}
}

// Accounts is a group of accounts that can be operated on concurrently.
type Accounts []*Account

// Sort sorts accounts by name.
func (s Accounts) Sort() Accounts {
	sort.Sort(byName(s))
	return s
}

// Map concurrently executes fn for each account. Any error returned by fn is
// stored in the associated account.
func (s Accounts) Map(fn func(i int, ac *Account) error) Accounts {
	fast.ForEachIO(len(s), func(i int) error {
		ac := s[i]
		if err := fn(i, ac); err != nil {
			if err == ErrNoCtl {
				ac.lostCtl()
			}
			ac.Err = err
		}
		return nil
	})
	return s
}

// ClearErr clears the error state of all accounts.
func (s Accounts) ClearErr() Accounts {
	for _, ac := range s {
		ac.Err = nil
	}
	return s
}

// Filter removes those accounts for which fn evaluates to false. This is done
// in-place without making a copy of the original slice.
func (s Accounts) Filter(fn func(ac *Account) bool) Accounts {
	i := 0
	for _, ac := range s {
		if fn(ac) {
			s[i] = ac
			i++
		}
	}
	s, x := s[:i], s[i:]
	for i := range x {
		x[i] = nil
	}
	return s
}

// RequireCtl ensures that all accounts have control information. Existing
// information is not refreshed.
func (s Accounts) RequireCtl() Accounts {
	var refresh Accounts
	for i, ac := range s {
		if !ac.HasCtl && ac.Err == nil {
			if len(refresh) == 0 {
				refresh = make(Accounts, 0, len(s)-i)
			}
			refresh = append(refresh, ac)
		}
	}
	if len(refresh) > 0 {
		refresh.Refresh()
	}
	return s
}

// Refresh retrieves current control information for all accounts.
func (s Accounts) Refresh() Accounts {
	return s.Map(func(_ int, ac *Account) error {
		if err := ac.ref.Get(ac.IAM); err != nil {
			ac.lostCtl()
			return err
		}
		ac.HasCtl = true
		ac.Ctl.copy(&ac.ref)
		return nil
	})
}

// Save saves control information for all accounts. When setting an owner, the
// caller must refresh account control information after a delay to confirm
// ownership.
func (s Accounts) Save() Accounts {
	return s.Map(func(_ int, ac *Account) error {
		if !ac.HasCtl {
			return ErrNoCtl
		}

		// Get current state and merge changes
		var cur Ctl
		if err := cur.Get(ac.IAM); err != nil {
			return err
		}
		if ac.Ctl.merge(&cur, &ac.ref); cur.eq(&ac.Ctl) {
			ac.ref = cur
			return nil
		}

		// To change the owner, current and reference states must match
		if cur.Owner != ac.Ctl.Owner && cur.Owner != ac.ref.Owner {
			ac.ref = cur
			return errCtlUpdate
		}

		// Update state
		if err := ac.Ctl.Set(ac.IAM); err != nil {
			return err
		}
		ac.ref.copy(&ac.Ctl)
		return nil
	})
}

// byName implements sort.Interface to sort accounts by name.
type byName Accounts

func (s byName) Len() int           { return len(s) }
func (s byName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool { return s[i].key.less(s[j].key) }

// sortKey is a string representation used for natural sorting. The original
// string is split into string-number pairs, with the string part converted to
// upper case for case-insensitive comparison (good enough for our purposes).
type sortKey [3]sortPair

type sortPair struct {
	s string
	n uint64
}

func natSortKey(s string) (k sortKey) {
	i, j, v, s := -1, 0, 0, strings.ToUpper(s)
	for s != "" {
		// Find the next run of digits s[i:j]
		for ; j < len(s); j++ {
			if s[j]-'0' <= 9 {
				if i < 0 {
					i = j
				}
			} else if i >= 0 {
				break
			}
		}
		if i < 0 {
			k[v].s = s
			break
		}
		if n, err := strconv.ParseUint(s[i:j], 10, 64); err == nil {
			k[v] = sortPair{s[:i], n}
			s, i, j = s[j:], -1, 0
			if v++; v == len(k)-1 {
				k[v].s = s
				break
			}
		} else {
			i = -1
		}
	}
	return
}

func (k sortKey) less(o sortKey) bool {
	for i := range k {
		if k[i].s != o[i].s {
			return k[i].s < o[i].s
		}
		if k[i].n != o[i].n {
			return k[i].n < o[i].n
		}
	}
	return false
}
