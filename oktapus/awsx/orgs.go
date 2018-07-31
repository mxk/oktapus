package awsx

import (
	"sync"
	"time"

	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
)

// CreateAccountResult contains the values returned by createAccount. If err is
// not nil, Account will contain the original name from CreateAccountInput.
type CreateAccountResult struct {
	*orgs.Account
	Err error
}

// CreateAccounts creates multiple accounts concurrently.
func CreateAccounts(c orgs.Organizations, in []*orgs.CreateAccountInput) <-chan CreateAccountResult {
	workers := 5 // Only 5 accounts may be created at the same time
	ich := make(chan *orgs.CreateAccountInput)
	rch := make(chan CreateAccountResult)
	var wg sync.WaitGroup
	for wg.Add(workers); workers > 0; workers-- {
		go func() {
			defer wg.Done()
			for in := range ich {
				ac, err := createAccount(c, in)
				if err != nil {
					// TODO: Retry if err is too many account creation ops
					if ac == nil {
						ac = &orgs.Account{Name: in.AccountName, Email: in.Email}
					} else if ac.Name == nil {
						ac.Name = in.AccountName
					}
				}
				rch <- CreateAccountResult{ac, err}
			}
		}()
	}
	go func() {
		defer close(rch)
		for _, ac := range in {
			ich <- ac
		}
		close(ich)
		wg.Wait()
	}()
	return rch
}

// createAccount creates a new account in the organization.
func createAccount(c orgs.Organizations, in *orgs.CreateAccountInput) (*orgs.Account, error) {
	out, err := c.CreateAccountRequest(in).Send()
	if err != nil {
		return nil, err
	}
	s := out.CreateAccountStatus
	reqID := orgs.DescribeCreateAccountStatusInput{
		CreateAccountRequestId: s.Id,
	}
	for {
		switch s.State {
		case orgs.CreateAccountStateInProgress:
			fast.Sleep(time.Second)
			out, err := c.DescribeCreateAccountStatusRequest(&reqID).Send()
			if err != nil {
				return nil, err
			}
			s = out.CreateAccountStatus
		case orgs.CreateAccountStateSucceeded:
			in := orgs.DescribeAccountInput{AccountId: s.AccountId}
			out, err := c.DescribeAccountRequest(&in).Send()
			if err != nil {
				return nil, err
			}
			return out.Account, nil
		default:
			return nil, awserr.New(string(s.FailureReason),
				"account creation failed", nil)
		}
	}
}
