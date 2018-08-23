package op

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
)

const (
	// ErrNoAccess indicates missing or invalid account credentials.
	ErrNoAccess = Error("account access denied")

	// ErrNoCtl indicates missing account control information.
	ErrNoCtl = Error("account control not initialized")

	// ErrCtlUpdate indicates that account control information was not saved.
	ErrCtlUpdate = Error("account control update interrupted")
)

// Flags contains account state flags.
type Flags uint32

// Flag bits.
const (
	CredsFlag Flags = 1 << iota // Credentials are valid
	LoadFlag                    // Control information load was attempted
	CtlFlag                     // Control information is valid
	OrgFlag                     // Account belongs to an organization
)

// Set sets the specified flag bits.
func (f *Flags) Set(b Flags) { *f |= b }

// Clear clears the specified flag bits.
func (f *Flags) Clear(b Flags) { *f &^= b }

// Test returns true if all specified flag bits are set.
func (f Flags) Test(b Flags) bool { return f&b == b }

// CredsValid returns true if the account credentials are valid.
func (f Flags) CredsValid() bool { return f&CredsFlag != 0 }

// CtlValid returns true if the account control information is valid.
func (f Flags) CtlValid() bool { return f&CtlFlag != 0 }

// Account maintains control information and provides IAM access for one AWS
// account.
type Account struct {
	Flags

	ID   string
	Name string
	IAM  iamx.Client
	Ctl  Ctl
	Err  error

	ref Ctl
	key sortKey
}

// NewAccount returns a new account with the given id and name.
func NewAccount(id, name string) *Account {
	if !account.IsID(id) {
		panic("op: invalid account id: " + id)
	}
	ac := &Account{ID: id, Name: name}
	ac.updateSortKey()
	return ac
}

// initAccounts initializes multiple accounts in an pre-allocated pool.
func initAccounts(pool []Account) Accounts {
	if len(pool) == 0 {
		return nil
	}
	acs := make(Accounts, len(pool))
	for i := range pool {
		ac := &pool[i]
		ac.ref.copy(&ac.Ctl)
		ac.updateSortKey()
		acs[i] = ac
	}
	return acs
}

// CredsProvider returns the credentials provider for account ac.
func (ac *Account) CredsProvider() *creds.Provider {
	return ac.IAM.Config.Credentials.(*creds.Provider)
}

// CtlUpdate updates account flags after control init/load/store operation.
func (ac *Account) CtlUpdate(err error) error {
	if ac.Set(CredsFlag | LoadFlag | CtlFlag); err != nil {
		e, ok := err.(awserr.RequestFailure)
		if ok && e.StatusCode() == http.StatusForbidden {
			// TODO: What if denied by IAM policy?
			ac.Clear(CredsFlag | CtlFlag)
		} else {
			ac.Clear(CtlFlag)
		}
	}
	return err
}

// updateSortKey updates account sort-by-name key.
func (ac *Account) updateSortKey() {
	ac.key = natSortKey(ac.Name + "\x00" + ac.ID)
}

// Accounts is a group of accounts that can be operated on concurrently.
type Accounts []*Account

// SortByID sorts accounts by ID.
func (s Accounts) SortByID() Accounts {
	sort.Sort(byID(s))
	return s
}

// SortByName sorts accounts by name.
func (s Accounts) SortByName() Accounts {
	sort.Sort(byName(s))
	return s
}

// Map concurrently executes fn for each account. Any error returned by fn is
// stored in the associated account.
func (s Accounts) Map(fn func(i int, ac *Account) error) Accounts {
	if len(s) > 0 {
		fast.ForEachIO(len(s), func(i int) error {
			ac := s[i]
			if err := fn(i, ac); err != nil {
				ac.Err = err
			}
			return nil
		})
	}
	return s
}

// Filter returns a new slice containing only those accounts for which fn
// evaluates to true.
func (s Accounts) Filter(fn func(ac *Account) bool) Accounts {
	var acs Accounts
	for i, ac := range s {
		if fn(ac) {
			if acs == nil {
				acs = make(Accounts, 0, len(s)-i)
			}
			acs = append(acs, ac)
		}
	}
	return acs
}

// EnsureCreds ensures that credentials of all accounts will remain valid for
// the specified duration, renewing them if necessary.
func (s Accounts) EnsureCreds(d time.Duration) Accounts {
	var ensure Accounts
	if d >= 0 {
		t := fast.Time().Add(d)
		for i, ac := range s {
			if cr, err := ac.CredsProvider().Creds(); err == nil {
				if creds.ValidUntil(&cr, t) {
					ac.Set(CredsFlag)
				} else {
					if ensure == nil {
						ensure = make(Accounts, 0, len(s)-i)
					}
					ensure = append(ensure, ac)
				}
			} else {
				ac.Clear(CredsFlag)
				ac.Err = err
			}
		}
	} else {
		ensure = s
	}
	ensure.Map(func(_ int, ac *Account) error {
		err := ac.CredsProvider().Ensure(d)
		if err == nil {
			ac.Set(CredsFlag)
		} else {
			ac.Clear(CredsFlag)
		}
		return err
	})
	return s
}

// InitCtl initializes control information of all accounts.
func (s Accounts) InitCtl() Accounts {
	return s.Map(func(_ int, ac *Account) error {
		if !ac.CtlValid() {
			// The error is always set to clear ErrNoCtl
			if ac.Err = ac.CtlUpdate(ac.Ctl.Init(ac.IAM)); ac.Err == nil {
				ac.ref.copy(&ac.Ctl)
			}
		}
		return nil
	})
}

// LoadCtl loads control information for accounts without LoadFlag set. If
// reload is true, the flag is ignored.
func (s Accounts) LoadCtl(reload bool) Accounts {
	var load Accounts
	if !reload {
		for i := range s {
			if s[i].Test(LoadFlag) {
				continue
			}
			load = make(Accounts, 0, len(s)-i)
			for _, ac := range s[i:] {
				if !ac.Test(LoadFlag) {
					load = append(load, ac)
				}
			}
			break
		}
	} else {
		load = s
	}
	load.Map(func(_ int, ac *Account) error {
		err := ac.CtlUpdate(ac.ref.Load(ac.IAM))
		ac.Ctl.copy(&ac.ref)
		return err
	})
	return s
}

// StoreCtl stores modified control information of all accounts. When setting an
// owner, the caller must refresh account control information after a delay to
// confirm ownership.
func (s Accounts) StoreCtl() Accounts {
	return s.Map(func(_ int, ac *Account) error {
		if !ac.CtlValid() {
			if ac.Err == nil {
				return ErrNoCtl
			}
			return nil
		}

		// Get current state and merge changes
		var cur Ctl
		if err := ac.CtlUpdate(cur.Load(ac.IAM)); err != nil {
			return err
		}
		if ac.Ctl.merge(&cur, &ac.ref); cur.eq(&ac.Ctl) {
			ac.ref = cur
			return nil
		}

		// To change the owner, current and reference states must match
		if cur.Owner != ac.Ctl.Owner && cur.Owner != ac.ref.Owner {
			ac.ref = cur
			return ErrCtlUpdate
		}

		// Update state
		err := ac.CtlUpdate(ac.Ctl.Store(ac.IAM))
		if err == nil {
			ac.ref.copy(&ac.Ctl)
		}
		return err
	})
}

// ClearErr clears the error state of all accounts.
func (s Accounts) ClearErr() Accounts {
	for _, ac := range s {
		ac.Err = nil
	}
	return s
}

// CredsOrErr sets the Err field of all accounts without valid credentials or an
// an existing error.
func (s Accounts) CredsOrErr() Accounts {
	for _, ac := range s {
		if !ac.CredsValid() && ac.Err == nil {
			ac.Err = ErrNoAccess
		}
	}
	return s
}

// CtlOrErr sets the Err field of all accounts without control information or an
// existing error.
func (s Accounts) CtlOrErr() Accounts {
	for _, ac := range s {
		if !ac.CtlValid() && ac.Err == nil {
			if !ac.CredsValid() {
				ac.Err = ErrNoAccess
			} else {
				ac.Err = ErrNoCtl
			}
		}
	}
	return s
}

// byID implements sort.Interface to sort accounts by ID.
type byID Accounts

func (s byID) Len() int           { return len(s) }
func (s byID) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byID) Less(i, j int) bool { return s[i].ID < s[j].ID }

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
	i, j, p, s := -1, 0, 0, strings.ToUpper(s)
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
			k[p].s = s
			break
		}
		if n, err := strconv.ParseUint(s[i:j], 10, 64); err == nil {
			k[p] = sortPair{s[:i], n}
			i, j, s = -1, 0, s[j:]
			if p++; p == len(k)-1 {
				k[p].s = s
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
