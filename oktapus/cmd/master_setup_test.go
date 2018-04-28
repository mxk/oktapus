package cmd

import (
	"io/ioutil"
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMasterSetup(t *testing.T) {
	ctx := newCtx()
	s := ctx.Sess.(*mock.Session)
	s.ChainRouter = append(s.ChainRouter, mock.NewDataTypeRouter(
		&iam.CreatePolicyOutput{},
		&iam.PutRolePolicyOutput{},
	))
	cmd := newCmd("master-setup", "-exec")
	prev := log.SetWriter(ioutil.Discard)
	defer log.SetWriter(prev)
	require.NoError(t, cmd.Run(ctx, nil))
	r := s.OrgsRouter().Account("").RoleRouter()
	assert.Contains(t, r, "OktapusOrganizationsProxy")
}
