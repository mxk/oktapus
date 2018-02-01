package internal

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError(t *testing.T) {
	assert.Nil(t, EncodableError(nil))

	src := fmt.Errorf("error @ %v", time.Now())
	enc := EncodableError(src)
	assert.NotEqual(t, src, enc)
	dec := encodeDecode(t, src)

	assert.True(t, dec == EncodableError(dec))
	assert.Equal(t, src.Error(), dec.Error())
}

func TestAWSError(t *testing.T) {
	src := awserr.New("code", "msg", nil)
	enc := EncodableError(src)
	assert.NotEqual(t, src, enc)
	dec := encodeDecode(t, src).(awserr.BatchedErrors)

	assert.True(t, dec == EncodableError(dec))
	assert.Equal(t, src.Error(), dec.Error())
	assert.Equal(t, src.Code(), dec.Code())
	assert.Equal(t, src.Message(), dec.Message())
	assert.Equal(t, src.OrigErr(), dec.OrigErr())
	assert.Equal(t, src.(awserr.BatchedErrors).OrigErrs(), dec.OrigErrs())
}

func TestOrigError(t *testing.T) {
	src := awserr.New("code", "msg", awserr.New("origCode", "origMsg", nil))
	enc := EncodableError(src)
	assert.NotEqual(t, src, enc)
	dec := encodeDecode(t, src).(awserr.BatchedErrors)

	assert.Equal(t, src.Error(), dec.Error())
	assert.Equal(t, src.Code(), dec.Code())
	assert.Equal(t, src.Message(), dec.Message())

	src = src.OrigErr().(awserr.BatchedErrors)
	dec = dec.OrigErr().(awserr.BatchedErrors)

	assert.Equal(t, src.Error(), dec.Error())
	assert.Equal(t, src.Code(), dec.Code())
	assert.Equal(t, src.Message(), dec.Message())
	assert.Equal(t, src.OrigErr(), dec.OrigErr())
	assert.Equal(t, src.(awserr.BatchedErrors).OrigErrs(), dec.OrigErrs())
}

func TestRequestError(t *testing.T) {
	src := awserr.NewRequestFailure(awserr.New("code", "msg", nil), 404, "id")
	enc := EncodableError(src)
	assert.NotEqual(t, src, enc)
	dec := encodeDecode(t, src).(awserr.RequestFailure)

	assert.True(t, dec == EncodableError(dec))
	assert.Equal(t, src.Error(), dec.Error())
	assert.Equal(t, src.Code(), dec.Code())
	assert.Equal(t, src.Message(), dec.Message())
	assert.Equal(t, src.OrigErr(), dec.OrigErr())
	assert.Equal(t, src.(awserr.BatchedErrors).OrigErrs(), dec.(awserr.BatchedErrors).OrigErrs())
	assert.Equal(t, src.StatusCode(), dec.StatusCode())
	assert.Equal(t, src.RequestID(), dec.RequestID())
}

func encodeDecode(t *testing.T, err error) error {
	type wrapper struct{ Err error }
	src := wrapper{EncodableError(err)}
	var buf bytes.Buffer
	var dst wrapper
	require.NoError(t, gob.NewEncoder(&buf).Encode(src))
	require.NoError(t, gob.NewDecoder(&buf).Decode(&dst))
	return dst.Err
}
