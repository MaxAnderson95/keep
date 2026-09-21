package launchd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// scriptedLaunchctl is a fake launchctl for the adapter's run seam. print
// answers from a queue, so a test can model a service that lingers in the
// domain for N polls after bootout returns — the real teardown window.
type scriptedLaunchctl struct {
	printOutputs []string // consumed one per print; "" means not found
	calls        []string
	bootoutOut   string
	bootoutErr   error
}

func (s *scriptedLaunchctl) run(args ...string) (string, error) {
	s.calls = append(s.calls, strings.Join(args, " "))
	switch args[0] {
	case "bootout":
		return s.bootoutOut, s.bootoutErr
	case "print":
		out := ""
		if len(s.printOutputs) > 0 {
			out = s.printOutputs[0]
			s.printOutputs = s.printOutputs[1:]
		}
		if out == "" {
			return `Could not find service "x" in domain for user gui: 501`, errors.New("exit 113")
		}
		return out, nil
	}
	return "", nil
}

func (s *scriptedLaunchctl) printCount() int {
	n := 0
	for _, c := range s.calls {
		if strings.HasPrefix(c, "print") {
			n++
		}
	}
	return n
}

// fastPolling shrinks the settle loop so tests do not sleep in real time.
func fastPolling(t *testing.T, timeout time.Duration) {
	t.Helper()
	origTimeout, origInterval := bootoutSettleTimeout, bootoutPollInterval
	bootoutSettleTimeout, bootoutPollInterval = timeout, time.Millisecond
	t.Cleanup(func() {
		bootoutSettleTimeout, bootoutPollInterval = origTimeout, origInterval
	})
}

const drainingPrint = "gui/501/keep.svc = {\n\tstate = SIGTERMed\n\tpid = 4242\n}"

// The bug this guards: launchctl bootout returns while the job is still
// SIGTERMed in the domain, and bootstrapping that label in the window fails
// with "5: Input/output error".
func TestBootoutWaitsForTeardown(t *testing.T) {
	fastPolling(t, time.Second)
	fake := &scriptedLaunchctl{printOutputs: []string{drainingPrint, drainingPrint, drainingPrint}}
	c := &Runtime{run: fake.run}

	if err := c.Unload(context.Background(), runtime.Target{Label: "keep.svc"}); err != nil {
		t.Fatalf("Bootout: %v", err)
	}
	// Three draining answers, then the not-found that ends the wait.
	if got := fake.printCount(); got != 4 {
		t.Errorf("print calls = %d, want 4 (polled until gone)", got)
	}
}

func TestBootoutReturnsImmediatelyWhenAlreadyGone(t *testing.T) {
	fastPolling(t, time.Second)
	fake := &scriptedLaunchctl{}
	c := &Runtime{run: fake.run}

	if err := c.Unload(context.Background(), runtime.Target{Label: "keep.svc"}); err != nil {
		t.Fatalf("Bootout: %v", err)
	}
	if got := fake.printCount(); got != 1 {
		t.Errorf("print calls = %d, want 1 (no drain to wait out)", got)
	}
}

func TestBootoutNotLoadedIsSuccessAndSkipsWait(t *testing.T) {
	fastPolling(t, time.Second)
	fake := &scriptedLaunchctl{
		bootoutOut: "Boot-out failed: 3: No such process",
		bootoutErr: errors.New("exit 3"),
	}
	c := &Runtime{run: fake.run}

	if err := c.Unload(context.Background(), runtime.Target{Label: "keep.svc"}); err != nil {
		t.Fatalf("Bootout: %v", err)
	}
	if got := fake.printCount(); got != 0 {
		t.Errorf("print calls = %d, want 0 (nothing was loaded)", got)
	}
}

func TestBootoutFailsWhenTeardownNeverCompletes(t *testing.T) {
	fastPolling(t, 20*time.Millisecond)
	fake := &scriptedLaunchctl{}
	for i := 0; i < 1000; i++ {
		fake.printOutputs = append(fake.printOutputs, drainingPrint)
	}
	c := &Runtime{run: fake.run}

	err := c.Unload(context.Background(), runtime.Target{Label: "keep.svc"})
	if err == nil {
		t.Fatal("want an error when the service never leaves the domain")
	}
	if !strings.Contains(err.Error(), "still tearing down") || !strings.Contains(err.Error(), "SIGTERMed") {
		t.Errorf("error should name the stuck state, got: %v", err)
	}
}

func TestBootoutRealErrorIsNotSwallowed(t *testing.T) {
	fastPolling(t, time.Second)
	fake := &scriptedLaunchctl{
		bootoutOut: "Boot-out failed: 1: Operation not permitted",
		bootoutErr: errors.New("exit 1"),
	}
	c := &Runtime{run: fake.run}

	err := c.Unload(context.Background(), runtime.Target{Label: "keep.svc"})
	if err == nil || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("want the launchctl error surfaced, got: %v", err)
	}
}

// A caller that cannot wait out a slow drain — an HTTP request whose client
// gave up, or a canceled update — abandons the watch instead of being pinned
// to another process's shutdown.
func TestBootoutWaitIsCancelable(t *testing.T) {
	fastPolling(t, time.Minute) // long enough that only cancellation can end it
	fake := &scriptedLaunchctl{}
	for i := 0; i < 100000; i++ {
		fake.printOutputs = append(fake.printOutputs, drainingPrint)
	}
	c := &Runtime{run: fake.run}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := c.Unload(ctx, runtime.Target{Label: "keep.svc"})
	if err == nil {
		t.Fatal("want an error when the wait is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %s, should return promptly", elapsed)
	}
}

// normalize is where launchctl's vocabulary becomes keep's. "Not in the domain"
// has to stay distinct from "known but stopped", because only the first one is
// drift.
func TestNormalizeMapsLaunchctlOntoState(t *testing.T) {
	exit := func(n int) PrintInfo { return PrintInfo{Loaded: true, LastExit: n, HasLastExit: true} }
	cases := []struct {
		name     string
		in       PrintInfo
		want     runtime.State
		wantPID  int
		wantExit *int
	}{
		{name: "not in the domain", in: PrintInfo{}, want: runtime.StateUnknown},
		{name: "loaded and idle", in: PrintInfo{Loaded: true}, want: runtime.StateStopped},
		{name: "running by state", in: PrintInfo{Loaded: true, State: "running"}, want: runtime.StateRunning},
		{name: "running by pid", in: PrintInfo{Loaded: true, State: "waiting", PID: 42, HasPID: true}, want: runtime.StateRunning, wantPID: 42},
		{name: "clean exit", in: exit(0), want: runtime.StateStopped, wantExit: intp(0)},
		{name: "failed exit", in: exit(3), want: runtime.StateFailed, wantExit: intp(3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize(tc.in)
			if got.State != tc.want {
				t.Errorf("State = %q, want %q", got.State, tc.want)
			}
			if got.PID != tc.wantPID {
				t.Errorf("PID = %d, want %d", got.PID, tc.wantPID)
			}
			switch {
			case tc.wantExit == nil && got.LastExit != nil:
				t.Errorf("LastExit = %d, want none", *got.LastExit)
			case tc.wantExit != nil && (got.LastExit == nil || *got.LastExit != *tc.wantExit):
				t.Errorf("LastExit = %v, want %d", got.LastExit, *tc.wantExit)
			}
		})
	}
}

// A scheduled run that failed still reports its exit code while the next run is
// live; status reads the code, not the state, to call that an error.
func TestNormalizeKeepsLastExitOnARunningUnit(t *testing.T) {
	got := normalize(PrintInfo{Loaded: true, State: "running", PID: 7, HasPID: true, LastExit: 1, HasLastExit: true})
	if got.State != runtime.StateRunning {
		t.Errorf("State = %q, want running", got.State)
	}
	if got.LastExit == nil || *got.LastExit != 1 {
		t.Errorf("LastExit = %v, want 1", got.LastExit)
	}
}
