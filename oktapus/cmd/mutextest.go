package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var mutextTestCli = register(&cli.Info{
	Name:    "mutex-test",
	Usage:   "[options] num-workers",
	Summary: "Test account owner mutex",
	MinArgs: 1,
	MaxArgs: 1,
	Hide:    true,
	New:     func() cli.Cmd { return mutexTestCmd{} },
})

const reportBatch = 10

var (
	verifyDelay  = 0 * time.Second
	confirmDelay = 30 * time.Second
	freeDelay    = 10 * time.Second
)

type mutexTestCmd struct{}

type delaySummary struct {
	Workers  int
	Delay    time.Duration
	Tests    int
	Failures int
}

type testResult struct {
	Num          int
	VerifyDelay  time.Duration
	FreeDelay    time.Duration
	Owners       int
	Misses       int
	Errors       int
	Setters      int
	AssumedOwner string
	FinalOwner   string
	Pass         bool
}

func (mutexTestCmd) Info() *cli.Info { return mutextTestCli }

func (mutexTestCmd) Main(args []string) error {
	return mutexTestCmd{}.Run(nil, args)
}

func (mutexTestCmd) Run(_ *op.Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	if n < 1 || err != nil {
		return cli.Error("number of workers must be > 0")
	}

	// Create verification IAM client
	env, err := external.NewEnvConfig()
	if err != nil {
		panic(err)
	}
	cfg, err := external.LoadDefaultAWSConfig(
		external.WithCredentialsValue(env.Credentials),
	)
	if err != nil {
		panic(err)
	}
	c := iam.New(cfg)
	initCtl := new(op.Ctl)
	if err := initCtl.Get(*c); err != nil {
		panic(err)
	} else if initCtl.Owner != "" {
		return fmt.Errorf("account is currently owned by %q", initCtl.Owner)
	}

	// Start workers
	run := sync.NewCond(new(sync.Mutex))
	ch := make(chan *workerResult)
	clients := make([]*http.Client, 0, n)
	for i := 0; i < n; i++ {
		cfg.HTTPClient = &http.Client{Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}}
		clients = append(clients, cfg.HTTPClient)
		go worker(fmt.Sprintf("W%.3d", i+1), *iam.New(cfg), run, ch)
	}

	// Run tests
	w := bufio.NewWriter(os.Stdout)
	summary := []*delaySummary{{Workers: n, Delay: verifyDelay}}
	var results []*testResult
	var owners, errors []*workerResult
	for testNum := 1; ; testNum++ {
		t := &testResult{
			Num:         testNum,
			VerifyDelay: verifyDelay,
			FreeDelay:   freeDelay,
		}
		fmt.Printf("\nTest #%d in...", t.Num)
		for i := 3; i > 0; i-- {
			fmt.Printf(" %d", i)
			fast.Sleep(time.Second)
		}
		fmt.Println(" 0")
		run.Broadcast()

		// Receive worker results
		owners, errors = owners[:0], errors[:0]
		done := 0
		for r := range ch {
			if r.step != stepGet {
				t.Setters++
			}
			if r.err != nil {
				t.Errors++
				errors = append(errors, r)
			} else if r.Owner == r.name {
				t.Owners++
				owners = append(owners, r)
			} else {
				t.Misses++
			}
			if done++; done == n {
				break
			}
		}

		// Report errors
		for i, r := range errors {
			fmt.Printf("ERROR(%s@%s): %v\n", r.name, r.step, r.err)
			if i == 2 {
				fmt.Printf("%d more errors\n", len(errors)-(i+1))
				break
			}
		}

		// Verify owner after a delay
		if len(owners) == 1 {
			r := owners[0]
			t.AssumedOwner = r.name
			fmt.Printf("Owner is %s, will verify in %v... ",
				r.name, confirmDelay)
			fast.Sleep(confirmDelay)
			if err := r.Get(*c); err != nil {
				panic(err)
			}
			if t.FinalOwner = r.Owner; t.AssumedOwner == t.FinalOwner {
				fmt.Printf("OK\n")
				t.Pass = true
			} else {
				fmt.Printf("FAIL: Owner is %s\n", t.FinalOwner)
			}
		} else {
			fmt.Printf("FAIL: %d owners\n", t.Owners)
		}

		// Free account
		if err := initCtl.Set(*c); err != nil {
			panic(err)
		}
		if freeDelay < verifyDelay {
			freeDelay = verifyDelay
		} else if t.Setters < n/2 && freeDelay < time.Minute {
			freeDelay += 5 * time.Second
		}
		fast.Sleep(freeDelay)

		// Update summary
		s := summary[len(summary)-1]
		if s.Tests++; !t.Pass {
			s.Failures++
		}

		// Print results in batches
		if results = append(results, t); len(results)%reportBatch == 0 {
			batch := results[len(results)-reportBatch:]
			w.WriteByte('\n')
			internal.NewPrinter(batch).Print(w, nil)
			w.WriteByte('\n')
			internal.NewPrinter(summary).Print(w, nil)
			w.Flush()

			// After each batch, close all connections
			for _, c := range clients {
				c.Transport.(*http.Transport).CloseIdleConnections()
			}
		}
		save("mutex-test-results.json", results)
		save("mutex-test-summary.json", summary)

		// Increase verification delay if needed
		if s.Tests >= 10 && s.Failures != 0 && verifyDelay < time.Minute {
			verifyDelay += time.Second
			s = &delaySummary{Workers: n, Delay: verifyDelay}
			summary = append(summary, s)
		}
	}
}

const (
	stepGet    = "get"
	stepSet    = "set"
	stepVerify = "verify"
)

type workerResult struct {
	op.Ctl

	name string
	step string
	err  error
}

func worker(name string, c iam.IAM, run *sync.Cond, ch chan<- *workerResult) {
	runtime.LockOSThread()
	for {
		r := &workerResult{name: name}
		run.L.Lock()
		run.Wait()
		run.L.Unlock()

		r.step = stepGet
		if r.err = r.Get(c); r.err != nil || r.Owner != "" {
			ch <- r
			continue
		}

		r.step = stepSet
		r.Owner = name
		if r.err = r.Set(c); r.err != nil {
			ch <- r
			continue
		}

		r.step = stepVerify
		fast.Sleep(verifyDelay)
		r.err = r.Get(c)
		ch <- r
	}
}

func save(name string, v interface{}) {
	f, err := os.Create(name)
	if err != nil {
		return
	}
	defer f.Close()
	b := bufio.NewWriter(f)
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	b.Flush()
}
