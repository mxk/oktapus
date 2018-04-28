package awsx

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

func TestAccountStatusEnum(t *testing.T) {
	assert.Empty(t, accountStatusEnum(nil))
	assert.Equal(t, accountStatusEnum(aws.String("abc")), "abc")
}

func TestJoinedMethodEnum(t *testing.T) {
	assert.Empty(t, joinedMethodEnum(nil))
	assert.Equal(t, joinedMethodEnum(aws.String("abc")), "abc")
}
