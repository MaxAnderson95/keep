package systemd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// scriptedSystemctl is a fake systemctl at the adapter's run seam. Tests script
// what `show` and `list-unit-files` answer and assert on the ordered call log;
// nothing here ever reaches a real service manager, which matters because CI
// runners have no systemd user session.
type scriptedSystemctl struct {
	calls     []string
	show      map[string]string // unit -> `systemctl show` output
	unitFiles string            // `list-unit-files` output
	version   string
	fail      map[string]string // command prefix -> output to fail with
}

func newScripted() *scriptedSystemctl {
	return &scriptedSystemctl{show: map[string]string{}, fail: map[string]string{}}
}

func (s *scriptedSystemctl) run(_ context.Context, args ...string) (string, error) {
	line := strings.Join(args, " ")
	s.calls = append(s.calls, line)
	for prefix, out := range s.fail {
		if strings.HasPrefix(line, prefix) {
			return out, errors.New("exit 1")
		}
	}
	switch args[0] {
	case "show":
		out, ok := s.show[args[1]]
		if !ok {
			return "Failed to get unit file state for " + args[1] + ": No such file or directory", errors.New("exit 1")
		}
		return out, nil
	case "list-unit-files":
		return s.unitFiles, nil
	case "--version":
		return s.version, nil
	}
	return "", nil
}

func (s *scriptedSystemctl) adapter() *Runtime {
	return &Runtime{run: s.run, artifactDir: "/units"}
}

func (s *scriptedSystemctl) called(want string) bool {
	for _, c := range s.calls {
		if c == want {
			return true
		}
	}
	return false
}

func (s *scriptedSystemctl) indexOf(want string) int {
	for i, c := range s.calls {
		if c == want {
			return i
		}
	}
	return -1
}

// systemd caches unit files, so starting without reloading first would run the
// previous generation of the artifact the Manager just rewrote.
func TestLoadReloadsBeforeStarting(t *testing.T) {
	s := newScripted()
	if err := s.adapter().Load(context.Background(), runtime.Target{Label: "keep.web"}); err != nil {
		t.Fatal(err)
	}
	reload, start := s.indexOf("daemon-reload"), s.indexOf("start keep.web.service")
	if reload < 0 || start < 0 {
		t.Fatalf("calls = %v", s.calls)
	}
	if reload > start {
		t.Errorf("daemon-reload must come before start: %v", s.calls)
	}
}

// A scheduled Service is driven by its timer: that is the unit keep starts,
// enables, and disables.
func TestScheduledTargetAddressesTheTimer(t *testing.T) {
	s := newScripted()
	r := s.adapter()
	target := runtime.Target{Label: "keep.backup", Scheduled: true}
	if err := r.Load(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := r.Hold(target); err != nil {
		t.Fatal(err)
	}
	if err := r.Release(target); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"start keep.backup.timer", "disable keep.backup.timer", "enable keep.backup.timer"} {
		if !s.called(want) {
			t.Errorf("missing %q in %v", want, s.calls)
		}
	}
}

// Stopping a timer only disarms it. Down promises the Service is actually
// stopped, so a run already in flight has to be stopped too.
func TestUnloadScheduledStopsTimerAndRun(t *testing.T) {
	s := newScripted()
	err := s.adapter().Unload(context.Background(), runtime.Target{Label: "keep.backup", Scheduled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stop keep.backup.timer", "stop keep.backup.service"} {
		if !s.called(want) {
			t.Errorf("missing %q in %v", want, s.calls)
		}
	}
}

func TestUnloadResidentStopsOnlyTheService(t *testing.T) {
	s := newScripted()
	if err := s.adapter().Unload(context.Background(), runtime.Target{Label: "keep.web"}); err != nil {
		t.Fatal(err)
	}
	if s.called("stop keep.web.timer") {
		t.Errorf("a resident Service has no timer: %v", s.calls)
	}
}

// Prune has already deleted the files and does not know whether the Service
// was scheduled, so Forget tries both unit forms and treats missing ones as
// nothing to do.
func TestForgetToleratesMissingUnits(t *testing.T) {
	s := newScripted()
	s.fail["stop keep.gone.timer"] = "Failed to stop keep.gone.timer: Unit keep.gone.timer not loaded."
	s.fail["disable keep.gone.timer"] = "Failed to disable unit: Unit file keep.gone.timer does not exist."
	s.fail["stop keep.gone.service"] = "Failed to stop keep.gone.service: Unit keep.gone.service not loaded."
	s.fail["disable keep.gone.service"] = "Failed to disable unit: Unit file keep.gone.service does not exist."

	if err := s.adapter().Forget(context.Background(), "keep.gone"); err != nil {
		t.Fatalf("Forget should tolerate units that are already gone: %v", err)
	}
	if s.indexOf("daemon-reload") < s.indexOf("disable keep.gone.service") {
		t.Errorf("daemon-reload must come last, after the files are gone: %v", s.calls)
	}
}

func TestForgetPropagatesRealFailures(t *testing.T) {
	s := newScripted()
	s.fail["stop keep.stuck.timer"] = "Failed to stop keep.stuck.timer: Access denied"
	err := s.adapter().Forget(context.Background(), "keep.stuck")
	if err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("want the systemctl error surfaced, got %v", err)
	}
}

func showOut(props map[string]string) string {
	var b strings.Builder
	for _, k := range []string{"LoadState", "ActiveState", "SubState", "MainPID", "ExecMainStatus", "Result"} {
		if v, ok := props[k]; ok {
			b.WriteString(k + "=" + v + "\n")
		}
	}
	return b.String()
}

func TestInfoStates(t *testing.T) {
	cases := []struct {
		name    string
		props   map[string]string
		absent  bool
		want    runtime.State
		wantPID int
	}{
		{
			name:   "unit the manager never heard of",
			absent: true,
			want:   runtime.StateUnknown,
		},
		{
			name: "not-found load state",
			props: map[string]string{
				"LoadState": "not-found", "ActiveState": "inactive", "MainPID": "0", "Result": "success",
			},
			want: runtime.StateUnknown,
		},
		{
			name: "running",
			props: map[string]string{
				"LoadState": "loaded", "ActiveState": "active", "SubState": "running",
				"MainPID": "4242", "ExecMainStatus": "0", "Result": "success",
			},
			want: runtime.StateRunning, wantPID: 4242,
		},
		{
			name: "stopped cleanly",
			props: map[string]string{
				"LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead",
				"MainPID": "0", "ExecMainStatus": "0", "Result": "success",
			},
			want: runtime.StateStopped,
		},
		{
			name: "failed",
			props: map[string]string{
				"LoadState": "loaded", "ActiveState": "failed", "SubState": "failed",
				"MainPID": "0", "ExecMainStatus": "127", "Result": "exit-code",
			},
			want: runtime.StateFailed,
		},
		{
			name: "armed timer is known and idle",
			props: map[string]string{
				"LoadState": "loaded", "ActiveState": "active", "SubState": "waiting", "MainPID": "0",
			},
			want: runtime.StateStopped,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScripted()
			if !tc.absent {
				s.show["keep.web.service"] = showOut(tc.props)
			}
			info, err := s.adapter().Info(runtime.Target{Label: "keep.web"})
			if err != nil {
				t.Fatal(err)
			}
			if info.State != tc.want {
				t.Errorf("State = %q, want %q", info.State, tc.want)
			}
			if info.PID != tc.wantPID {
				t.Errorf("PID = %d, want %d", info.PID, tc.wantPID)
			}
		})
	}
}

// The timer says whether the Service is armed; the service unit says how the
// last run ended. status turns that exit code into "error" while the timer
// keeps reporting idle.
func TestInfoScheduledReadsTimerStateAndRunResult(t *testing.T) {
	s := newScripted()
	s.show["keep.backup.timer"] = showOut(map[string]string{
		"LoadState": "loaded", "ActiveState": "active", "SubState": "waiting", "MainPID": "0", "Result": "success",
	})
	s.show["keep.backup.service"] = showOut(map[string]string{
		"LoadState": "loaded", "ActiveState": "failed", "SubState": "failed",
		"MainPID": "0", "ExecMainStatus": "3", "Result": "exit-code",
	})

	info, err := s.adapter().Info(runtime.Target{Label: "keep.backup", Scheduled: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != runtime.StateStopped {
		t.Errorf("State = %q, want stopped: the timer is armed and waiting", info.State)
	}
	if info.LastExit == nil || *info.LastExit != 3 {
		t.Errorf("LastExit = %v, want 3 from the run unit", info.LastExit)
	}
}

// An unknown timer means the Service is not installed at all; there is no run
// unit worth consulting.
func TestInfoScheduledUnknownTimerSkipsTheRunUnit(t *testing.T) {
	s := newScripted()
	info, err := s.adapter().Info(runtime.Target{Label: "keep.backup", Scheduled: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != runtime.StateUnknown {
		t.Errorf("State = %q, want unknown", info.State)
	}
	if s.called("show keep.backup.service -p " + showProperties) {
		t.Errorf("should not have consulted the run unit: %v", s.calls)
	}
}

func TestHeldParsesMixedUnitFiles(t *testing.T) {
	s := newScripted()
	s.unitFiles = strings.Join([]string{
		"keep.web.service              enabled  enabled",
		"keep.held.service             disabled disabled",
		// A scheduled Service: only the timer is ever enabled or disabled, so
		// its state is the Service's state.
		"keep.backup.service           static   -",
		"keep.backup.timer             disabled disabled",
		"vendor-thing.service          enabled  enabled",
		"",
	}, "\n")

	held, err := s.adapter().Held()
	if err != nil {
		t.Fatal(err)
	}
	if held["keep.web"] {
		t.Error("keep.web is enabled, not held")
	}
	if !held["keep.held"] {
		t.Error("keep.held is disabled, so it is held")
	}
	if !held["keep.backup"] {
		t.Error("the timer is disabled, so the scheduled Service is held")
	}
	if held["vendor-thing"] {
		t.Error("an unmanaged unit that is enabled is not held")
	}
}

// The timer's state wins however systemctl happens to order its output.
func TestHeldTimerWinsRegardlessOfOrder(t *testing.T) {
	for _, order := range [][]string{
		{"keep.backup.timer disabled disabled", "keep.backup.service static -"},
		{"keep.backup.service static -", "keep.backup.timer disabled disabled"},
	} {
		s := newScripted()
		s.unitFiles = strings.Join(order, "\n")
		held, err := s.adapter().Held()
		if err != nil {
			t.Fatal(err)
		}
		if !held["keep.backup"] {
			t.Errorf("timer state should win for order %v", order)
		}
	}
}

// `systemctl --user --version` prints a version without ever contacting the
// user bus, so the probe has to ask the manager something only it can answer.
func TestProbeAsksTheManagerNotTheBinary(t *testing.T) {
	s := newScripted()
	if err := s.adapter().Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.called("show-environment") {
		t.Errorf("probe should query the manager's environment, got %v", s.calls)
	}

	s = newScripted()
	s.fail["show-environment"] = "Failed to connect to bus: No medium found"
	err := s.adapter().Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "No medium found") {
		t.Fatalf("want the bus failure surfaced, got %v", err)
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]int{
		"systemd 255 (255.4-1ubuntu8.4)\n+PAM +AUDIT": 255,
		"systemd 239 (239-78.el8)":                    239,
		"systemd 250~rc1 (250~rc1-1)":                 250,
		"nothing useful here":                         0,
	}
	for in, want := range cases {
		if got := parseVersion(in); got != want {
			t.Errorf("parseVersion(%q) = %d, want %d", in, got, want)
		}
	}
}

// systemd keeps a failed unit in memory after its file is deleted, so without
// this a pruned Service lingers in `systemctl --user --failed` until reboot.
func TestForgetClearsResidualFailedState(t *testing.T) {
	s := newScripted()
	if err := s.adapter().Forget(context.Background(), "keep.gone"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reset-failed keep.gone.timer", "reset-failed keep.gone.service"} {
		if !s.called(want) {
			t.Errorf("missing %q in %v", want, s.calls)
		}
	}
}

// There is usually nothing to reset, and that must not fail the prune.
func TestForgetIgnoresResetFailedErrors(t *testing.T) {
	s := newScripted()
	s.fail["reset-failed"] = "Failed to reset failed state: Unit keep.gone.service not loaded."
	if err := s.adapter().Forget(context.Background(), "keep.gone"); err != nil {
		t.Fatalf("reset-failed is cleanup, not a precondition: %v", err)
	}
}
