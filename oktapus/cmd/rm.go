package cmd

import (
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/mxk/cloudcover/oktapus/op"
	"github.com/mxk/cloudcover/x/awsx"
	"github.com/mxk/cloudcover/x/cli"
	"github.com/mxk/cloudcover/x/fast"
	"github.com/mxk/cloudcover/x/iamx"
)

var rmCli = cli.Main.Add(&cli.Info{
	Name:    "rm",
	Usage:   "account-spec {role|user} name [name ...]",
	Summary: "Remove IAM users and roles",
	MinArgs: 3,
	New:     func() cli.Cmd { return &rmCmd{} },
})

type rmCmd struct {
	OutFmt
	Spec  string
	Type  string
	Names []string
}

func (*rmCmd) Info() *cli.Info { return rmCli }

func (*rmCmd) Help(w *cli.Writer) {
	w.Text("Remove IAM users and roles.")
	accountSpecHelp(w)
}

func (cmd *rmCmd) Main(args []string) error {
	cmd.Spec, cmd.Type, cmd.Names = args[0], args[1], args[2:]
	return op.RunAndPrint(cmd)
}

func (cmd *rmCmd) Run(ctx *op.Ctx) (interface{}, error) {
	var rm func(c iamx.Client, r string) error
	switch cmd.Type {
	case "role":
		rm = func(c iamx.Client, r string) error { return c.DeleteRole(r) }
	case "user":
		rm = func(c iamx.Client, r string) error { return c.DeleteUser(r) }
	default:
		return nil, cli.Errorf("invalid resource type %q", cmd.Type)
	}
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	out := make([]*rmOutput, len(acs)*len(cmd.Names))
	if len(out) == 0 {
		return nil, nil
	}
	compact := false
	acs.EnsureCreds(minDur).Map(func(i int, ac *op.Account) error {
		i *= len(cmd.Names)
		out := out[i : i+len(cmd.Names)]
		if !ac.CredsValid() {
			out[0] = &rmOutput{
				Account: ac.ID,
				Name:    ac.Name,
				Result:  "ERROR: " + explainError(ac.Err),
			}
			compact = true
			return nil
		}
		return fast.ForEachIO(len(cmd.Names), func(i int) error {
			ro := &rmOutput{
				Account:  ac.ID,
				Name:     ac.Name,
				Resource: cmd.Names[i],
			}
			if err := rm(ac.IAM, cmd.Names[i]); err == nil {
				ro.Result = "OK"
			} else if awsx.ErrCode(err) == iam.ErrCodeNoSuchEntityException {
				ro.Result = "NOT FOUND"
			} else {
				ro.Result = "ERROR: " + explainError(err)
			}
			out[i] = ro
			return nil
		})
	})
	if compact {
		i := 0
		for _, ro := range out {
			if ro != nil {
				out[i] = ro
				i++
			}
		}
		out = out[:i]
	}
	return out, nil
}

type rmOutput struct{ Account, Name, Resource, Result string }
