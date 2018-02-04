package op

import (
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/iam"
)

// Account is an account in an AWS organization.
type Account struct {
	*awsgw.Account
	*Ctl

	IAM *iam.IAM
	Err error

	cp  awsgw.CredsProvider
	ref Ctl
}

// Creds returns temporary account credentials.
func (ac *Account) Creds(renew bool) (*awsgw.StaticCreds, error) {
	if renew {
		ac.cp.Reset()
	}
	if _, err := ac.IAM.Config.Credentials.Get(); err != nil {
		return nil, err
	}
	return ac.cp.Save(), nil
}

// Accounts is a group of accounts that can be operated on concurrently.
type Accounts []*Account

// Sort sorts accounts by name.
func (s Accounts) Sort() Accounts {
	// TODO: Natural number sorting
	sort.Sort(byName(s))
	return s
}

// Shuffle randomizes account order.
func (s Accounts) Shuffle() Accounts {
	if len(s) > math.MaxInt32 {
		panic("you have way too many accounts")
	}
	for i := int32(len(s) - 1); i > 0; i-- {
		j := rand.Int31n(i + 1)
		s[i], s[j] = s[j], s[i]
	}
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

// RequireIAM ensures that all accounts have an IAM client.
func (s Accounts) RequireIAM(c *awsgw.Client) Accounts {
	sess := c.ConfigProvider()
	var cfg aws.Config
	for _, ac := range s {
		if ac.IAM == nil {
			ac.cp = c.CredsProvider(ac.ID)
			cfg.Credentials = credentials.NewCredentials(ac.cp)
			ac.IAM = iam.New(sess, &cfg)
		}
	}
	return s
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
	return s.Apply(func(ac *Account) {
		if ac.Err = ac.ref.Get(ac.IAM); ac.Err != nil {
			ac.Ctl = nil
		} else {
			if ac.Ctl == nil {
				ac.Ctl = new(Ctl)
			}
			*ac.Ctl = ac.ref
		}
	})
}

// Save updates control information for all accounts. When changing the owner,
// the caller must refresh account control information after a delay to confirm
// owner status.
func (s Accounts) Save() Accounts {
	return s.Apply(func(ac *Account) {
		if ac.Ctl == nil {
			if ac.Err == nil {
				ac.Err = ErrNoCtl
			}
			return
		}

		// Get current state and merge changes
		var cur Ctl
		if ac.Err = cur.Get(ac.IAM); ac.Err != nil {
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
		if ac.Err = ac.Ctl.Set(ac.IAM); ac.Err != nil {
			ac.ref = cur
		} else {
			ac.ref = *ac.Ctl
		}
	})
}

// Apply executes fn on each account concurrently.
func (s Accounts) Apply(fn func(ac *Account)) Accounts {
	// The number of goroutines is fixed because the work is IO-bound. It simply
	// sets the number of API requests that can be in-flight at any given time.
	n := 50
	if len(s) < n {
		if n = len(s); n == 0 {
			return s
		}
	}
	var wg sync.WaitGroup
	ch := make(chan *Account, n)
	for i := n; i > 0; i-- {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ac := range ch {
				fn(ac)
			}
		}()
	}
	for _, ac := range s {
		ch <- ac
	}
	close(ch)
	wg.Wait()
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
