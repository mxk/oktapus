package internal

import (
	"encoding/gob"

	"github.com/aws/aws-sdk-go-v2/aws/awserr"
)

func init() {
	gob.Register(new(strErr))
	gob.Register(new(awsErr))
	gob.Register(new(awsReqErr))
}

// GobRegistered is an interface implemented by types that can be safely encoded
// in a gob stream.
type GobRegistered interface {
	GobRegistered()
}

// RegisteredError returns a gob-registered error implementation.
func RegisteredError(err error) error {
	switch err := err.(type) {
	case nil, GobRegistered:
		return err
	case awserr.Error:
		var orig []error
		if b, ok := err.(awserr.BatchedErrors); ok {
			orig = b.OrigErrs()
		} else if e := err.OrigErr(); e != nil {
			orig = []error{e}
		}
		for i := range orig {
			orig[i] = RegisteredError(orig[i])
		}
		e := &awsErr{
			Err:  err.Code(),
			Msg:  err.Message(),
			Orig: orig,
		}
		if r, ok := err.(awserr.RequestFailure); ok {
			return &awsReqErr{
				Err:    e,
				Status: r.StatusCode(),
				ReqID:  r.RequestID(),
			}
		}
		return e
	default:
		return &strErr{err.Error()}
	}
}

type strErr struct{ Err string }

func (e *strErr) Error() string  { return e.Err }
func (e *strErr) GobRegistered() {}

type awsErr struct {
	Err  string
	Msg  string
	Orig []error
	err  awserr.BatchedErrors
}

func (e *awsErr) Error() string     { return e.getErr().Error() }
func (e *awsErr) Code() string      { return e.getErr().Code() }
func (e *awsErr) Message() string   { return e.getErr().Message() }
func (e *awsErr) OrigErr() error    { return e.getErr().OrigErr() }
func (e *awsErr) OrigErrs() []error { return e.getErr().OrigErrs() }
func (e *awsErr) GobRegistered()    {}

func (e *awsErr) getErr() awserr.BatchedErrors {
	if e.err == nil {
		e.err = awserr.NewBatchError(e.Err, e.Msg, e.Orig)
	}
	return e.err
}

type awsReqErr struct {
	Err    awserr.Error
	Status int
	ReqID  string
	err    requestFailure
}

func (e *awsReqErr) Error() string     { return e.getErr().Error() }
func (e *awsReqErr) Code() string      { return e.getErr().Code() }
func (e *awsReqErr) Message() string   { return e.getErr().Message() }
func (e *awsReqErr) OrigErr() error    { return e.getErr().OrigErr() }
func (e *awsReqErr) OrigErrs() []error { return e.getErr().OrigErrs() }
func (e *awsReqErr) StatusCode() int   { return e.getErr().StatusCode() }
func (e *awsReqErr) RequestID() string { return e.getErr().RequestID() }
func (e *awsReqErr) GobRegistered()    {}

func (e *awsReqErr) getErr() requestFailure {
	if e.err == nil {
		e.err = awserr.NewRequestFailure(e.Err, e.Status, e.ReqID).(requestFailure)
	}
	return e.err
}

type requestFailure interface {
	awserr.RequestFailure
	OrigErrs() []error
}
