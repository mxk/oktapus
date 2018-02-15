package awsx

import (
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDelTmpUsers(t *testing.T) {
	s := mock.NewSession()
	c := iam.New(s)

	path := "/test/"
	require.NoError(t, DeleteUsers(c, path))

	r := s.OrgsRouter().Account("").UserRouter()
	r["a"] = &mock.User{User: iam.User{
		Arn:      aws.String(mock.UserARN("", "a")),
		Path:     aws.String("/"),
		UserName: aws.String("a"),
	}}
	r["b"] = &mock.User{
		User: iam.User{
			Arn:      aws.String(mock.UserARN("", "b")),
			Path:     aws.String(path),
			UserName: aws.String("b"),
		},
		AttachedPolicies: map[string]string{
			mock.PolicyARN("", "TestPolicy"): "TestPolicy",
		},
		AccessKeys: []*iam.AccessKeyMetadata{{
			AccessKeyId: aws.String(mock.AccessKeyID),
			Status:      aws.String(iam.StatusTypeActive),
			UserName:    aws.String("b"),
		}},
	}

	require.NoError(t, DeleteUsers(c, path))
	assert.Contains(t, r, "a")
	assert.NotContains(t, r, "b")
}

func TestDelTmpRoles(t *testing.T) {
	s := mock.NewSession()
	c := iam.New(s)

	path := "/temp/"
	require.NoError(t, DeleteRoles(c, path))

	r := s.OrgsRouter().Account("").RoleRouter()
	r["a"] = &mock.Role{Role: iam.Role{
		Arn:      aws.String(mock.RoleARN("", "a")),
		Path:     aws.String("/"),
		RoleName: aws.String("a"),
	}}
	r["b"] = &mock.Role{
		Role: iam.Role{
			Arn:      aws.String(mock.RoleARN("", "b")),
			Path:     aws.String(path),
			RoleName: aws.String("b"),
		},
		AttachedPolicies: map[string]string{
			mock.PolicyARN("", "AttachedPolicy1"): "AttachedPolicy1",
			mock.PolicyARN("", "AttachedPolicy2"): "AttachedPolicy2",
		},
		InlinePolicies: map[string]string{
			"InlinePolicy1": "{}",
			"InlinePolicy2": "{}",
		},
	}

	require.NoError(t, DeleteRoles(c, path))
	assert.Contains(t, r, "a")
	assert.NotContains(t, r, "b")
}
