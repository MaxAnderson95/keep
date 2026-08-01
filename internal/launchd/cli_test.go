package launchd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedLaunchctl is a fake launchctl for the CLI adapter's run seam. print
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
	c := &CLI{run: fake.run}

	if err := c.Bootout(context.Background(), "keep.svc"); err != nil {
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
	c := &CLI{run: fake.run}

	if err := c.Bootout(context.Background(), "keep.svc"); err != nil {
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
	c := &CLI{run: fake.run}

	if err := c.Bootout(context.Background(), "keep.svc"); err != nil {
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
	c := &CLI{run: fake.run}

	err := c.Bootout(context.Background(), "keep.svc")
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
	c := &CLI{run: fake.run}

	err := c.Bootout(context.Background(), "keep.svc")
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
	c := &CLI{run: fake.run}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := c.Bootout(ctx, "keep.svc")
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
