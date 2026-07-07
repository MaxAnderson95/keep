package keep

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// updateTestManager builds a Manager with a fake launchd, temp agents dir,
// and — critically for update runs — temp state and log directories.
func updateTestManager(t *testing.T, yaml string) (*Manager, *fakeController) {
	t.Helper()
	cfg := mustParse(t, yaml)
	cfg.Defaults.LogDir = t.TempDir()
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	m.stateDir = t.TempDir()
	return m, ctl
}

const updYAML = `
services:
  svc:
    command: /bin/echo serve
    update:
      - /bin/echo updated-one
      - /bin/echo updated-two
`

func TestUpdateSuccessRestoresUp(t *testing.T) {
	m, ctl := updateTestManager(t, updYAML)
	s, _ := m.Cfg.Service("svc")

	var out bytes.Buffer
	res, err := m.Update(context.Background(), s, &out)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out.String())
	}
	if !res.OK || res.StayedHeld || res.TimedOut {
		t.Fatalf("result = %+v, want OK", res)
	}

	// Output captured live, in order.
	text := out.String()
	one := strings.Index(text, "updated-one")
	two := strings.Index(text, "updated-two")
	if one < 0 || two < 0 || two < one {
		t.Fatalf("output missing or misordered command output:\n%s", text)
	}
	if !strings.Contains(text, "==> update svc: success") {
		t.Fatalf("output missing success footer:\n%s", text)
	}

	// Down before the commands, Up after (ADR-0006).
	want := []string{"disable keep.svc", "bootout keep.svc", "enable keep.svc", "bootstrap keep.svc"}
	if len(ctl.calls) < len(want) {
		t.Fatalf("calls = %v", ctl.calls)
	}
	for i, c := range want {
		if ctl.calls[i] != c {
			t.Fatalf("calls[%d] = %q, want %q (all: %v)", i, ctl.calls[i], c, ctl.calls)
		}
	}

	// The same output landed in the update log.
	logData, err := os.ReadFile(m.Cfg.UpdateLogPath(s))
	if err != nil {
		t.Fatalf("update log: %v", err)
	}
	if !strings.Contains(string(logData), "updated-one") {
		t.Fatalf("update log missing output:\n%s", logData)
	}

	// Lock released.
	if m.UpdateInProgress(s) {
		t.Fatal("lock still held after a finished run")
	}
}

func TestUpdateFailureLeavesHold(t *testing.T) {
	m, ctl := updateTestManager(t, `
services:
  svc:
    command: /bin/echo serve
    update:
      - /usr/bin/false
      - /bin/echo never-runs
`)
	s, _ := m.Cfg.Service("svc")

	var out bytes.Buffer
	res, err := m.Update(context.Background(), s, &out)
	if err == nil {
		t.Fatal("update with a failing command should error")
	}
	if res.OK {
		t.Fatalf("result = %+v, want failure", res)
	}
	// Fail closed: down'd, never brought back (no enable after the disable).
	if !ctl.disabled["keep.svc"] {
		t.Fatal("service not left disabled (held) after a failed update")
	}
	if ctl.didCall("enable keep.svc") {
		t.Fatalf("failed update still brought the service Up: %v", ctl.calls)
	}
	text := out.String()
	if strings.Contains(text, "never-runs") {
		t.Fatalf("run continued past a failed command:\n%s", text)
	}
	if !strings.Contains(text, "FAILED") {
		t.Fatalf("output missing failure footer:\n%s", text)
	}
}

func TestUpdateHeldServiceStaysDown(t *testing.T) {
	m, ctl := updateTestManager(t, updYAML)
	s, _ := m.Cfg.Service("svc")
	ctl.disabled["keep.svc"] = true // a deliberate prior Hold

	var out bytes.Buffer
	res, err := m.Update(context.Background(), s, &out)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out.String())
	}
	if !res.OK || !res.StayedHeld {
		t.Fatalf("result = %+v, want OK+StayedHeld", res)
	}
	if ctl.didCall("enable keep.svc") {
		t.Fatalf("update undid a deliberate hold: %v", ctl.calls)
	}
	if !ctl.disabled["keep.svc"] {
		t.Fatal("service no longer held after update")
	}
}

func TestUpdateTimeoutKillsAndFails(t *testing.T) {
	old := updateKillGrace
	updateKillGrace = 2 * time.Second
	defer func() { updateKillGrace = old }()

	m, ctl := updateTestManager(t, `
services:
  svc:
    command: /bin/echo serve
    update:
      - /bin/sleep 30
    update_timeout: 200ms
`)
	s, _ := m.Cfg.Service("svc")

	var out bytes.Buffer
	start := time.Now()
	res, err := m.Update(context.Background(), s, &out)
	if err == nil {
		t.Fatal("timed-out update should error")
	}
	if !res.TimedOut {
		t.Fatalf("result = %+v, want TimedOut", res)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s, kill did not work", elapsed)
	}
	if ctl.didCall("enable keep.svc") {
		t.Fatalf("timed-out update still brought the service Up: %v", ctl.calls)
	}
	if !strings.Contains(out.String(), "TIMED OUT") {
		t.Fatalf("output missing timeout marker:\n%s", out.String())
	}
}

func TestUpdateLockContention(t *testing.T) {
	m, _ := updateTestManager(t, updYAML)
	s, _ := m.Cfg.Service("svc")

	lock, err := m.acquireUpdateLock(s.Name)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lock.release()

	if !m.UpdateInProgress(s) {
		t.Fatal("probe should see the held lock")
	}
	if _, err := m.Update(context.Background(), s, &bytes.Buffer{}); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("concurrent update error = %v, want ErrUpdateInProgress", err)
	}

	lock.release()
	if m.UpdateInProgress(s) {
		t.Fatal("probe should be clear after release")
	}
}

func TestUpdateNoCommands(t *testing.T) {
	m, _ := updateTestManager(t, `
services:
  svc:
    command: /bin/echo serve
`)
	s, _ := m.Cfg.Service("svc")
	if _, err := m.Update(context.Background(), s, &bytes.Buffer{}); !errors.Is(err, ErrNoUpdateCommands) {
		t.Fatalf("err = %v, want ErrNoUpdateCommands", err)
	}
}

func TestUpdateRunsWithServiceEnv(t *testing.T) {
	m, _ := updateTestManager(t, `
services:
  svc:
    command: /bin/echo serve
    env:
      KEEP_UPDATE_TEST_VAR: from-service-env
    update:
      - /usr/bin/env
`)
	s, _ := m.Cfg.Service("svc")

	var out bytes.Buffer
	if _, err := m.Update(context.Background(), s, &out); err != nil {
		t.Fatalf("update: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "KEEP_UPDATE_TEST_VAR=from-service-env") {
		t.Fatalf("update command did not get the service env:\n%s", out.String())
	}
}

func TestStatusReportsUpdating(t *testing.T) {
	m, _ := updateTestManager(t, updYAML)
	s, _ := m.Cfg.Service("svc")

	// Idle: has_update true, updating false.
	statuses, err := m.Status([]string{"svc"})
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].HasUpdate || statuses[0].Updating {
		t.Fatalf("status = %+v, want HasUpdate && !Updating", statuses[0])
	}

	// While the lock is held, health flips to updating.
	lock, err := m.acquireUpdateLock(s.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	statuses, err = m.Status([]string{"svc"})
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].Updating || statuses[0].Health != HealthUpdating {
		t.Fatalf("status = %+v, want Updating/HealthUpdating", statuses[0])
	}
	if statuses[0].Drift {
		t.Fatalf("an in-flight update must not read as drift: %+v", statuses[0])
	}
}

func TestUpdateLogTargetIncluded(t *testing.T) {
	m, _ := updateTestManager(t, updYAML)
	targets, err := m.LogTargets([]string{"svc"})
	if err != nil {
		t.Fatal(err)
	}
	var streams []string
	for _, tgt := range targets {
		streams = append(streams, tgt.Stream)
	}
	if len(targets) != 3 || streams[2] != "update" {
		t.Fatalf("log targets = %v, want out/err/update", streams)
	}
}
