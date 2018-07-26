package awsx

import (
	"sync"
	"time"

	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	orgsif "github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
)

// CreateAccountResult contains the values returned by createAccount. If err is
// not nil, Account will contain the original name from CreateAccountInput.
type CreateAccountResult struct {
	*orgs.Account
	Err error
}

// CreateAccounts creates multiple accounts concurrently.
func CreateAccounts(c orgsif.OrganizationsAPI, in []*orgs.CreateAccountInput) <-chan CreateAccountResult {
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
func createAccount(c orgsif.OrganizationsAPI, in *orgs.CreateAccountInput) (*orgs.Account, error) {
	out, err := c.CreateAccount(in)
	if err != nil {
		return nil, err
	}
	s := out.CreateAccountStatus
	reqID := orgs.DescribeCreateAccountStatusInput{
		CreateAccountRequestId: s.Id,
	}
	for {
		switch aws.StringValue(s.State) {
		case orgs.CreateAccountStateInProgress:
			fast.Sleep(time.Second)
			out, err := c.DescribeCreateAccountStatus(&reqID)
			if err != nil {
				return nil, err
			}
			s = out.CreateAccountStatus
		case orgs.CreateAccountStateSucceeded:
			in := orgs.DescribeAccountInput{AccountId: s.AccountId}
			out, err := c.DescribeAccount(&in)
			return out.Account, err
		default:
			return nil, awserr.New(aws.StringValue(s.FailureReason),
				"account creation failed", nil)
		}
	}
}
