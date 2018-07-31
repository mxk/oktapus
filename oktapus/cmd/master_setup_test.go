package cmd

import (
	"io/ioutil"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMasterSetup(t *testing.T) {
	ctx, s := newCtx()
	s.ChainRouter = append(s.ChainRouter, mock.NewDataTypeRouter(
		&iam.CreatePolicyOutput{},
		&iam.PutRolePolicyOutput{},
	))
	cmd := masterSetupCmd{Exec: true}
	prev := log.SetWriter(ioutil.Discard)
	defer log.SetWriter(prev)
	require.NoError(t, cmd.Run(ctx, nil))
	r := s.OrgsRouter().Account("").RoleRouter()
	assert.Contains(t, r, "OktapusOrganizationsProxy")
}
