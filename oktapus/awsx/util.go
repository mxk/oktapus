package awsx

import "github.com/aws/aws-sdk-go-v2/aws/awserr"

// IsAccountID returns true if id is a valid AWS account ID.
func IsAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 11; i >= 0; i-- {
		if c := id[i]; c < '0' || '9' < c {
			return false
		}
	}
	return true
}

// IsCode returns true if err is an awserr.Error with the given code.
func IsCode(err error, code string) bool {
	e, ok := err.(awserr.Error)
	return ok && e.Code() == code
}

// IsStatus returns true if err is an awserr.RequestFailure with the given
// status code.
func IsStatus(err error, status int) bool {
	e, ok := err.(awserr.RequestFailure)
	return ok && e.StatusCode() == status
}
