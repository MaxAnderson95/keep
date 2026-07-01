package keep

import (
	"testing"

	"github.com/MaxAnderson95/keep/internal/launchd"
)

func statusFor(t *testing.T, m *Manager, name string) ServiceStatus {
	t.Helper()
	sts, err := m.Status([]string{name})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(sts) != 1 {
		t.Fatalf("want 1 status, got %d", len(sts))
	}
	return sts[0]
}

func TestStatusRunning(t *testing.T) {
	ctl := newFakeController()
	ctl.loaded["keep.web"] = launchd.PrintInfo{Loaded: true, State: "running", PID: 4242, HasPID: true}
	m := testManager(t, mustParse(t, oneResident(t)), ctl)
	st := statusFor(t, m, "web")
	if st.Health != HealthRunning {
		t.Errorf("Health = %q, want running", st.Health)
	}
	if st.PID != 4242 || st.Drift {
		t.Errorf("unexpected status %+v", st)
	}
}

func TestStatusHeld(t *testing.T) {
	ctl := newFakeController()
	ctl.disabled["keep.web"] = true
	m := testManager(t, mustParse(t, oneResident(t)), ctl)
	st := statusFor(t, m, "web")
	if st.Health != HealthHeld || !st.Drift || !st.Held {
		t.Errorf("want held+drift, got %+v", st)
	}
}

func TestStatusNotLoadedIsDrift(t *testing.T) {
	ctl := newFakeController() // enabled, not disabled, not loaded
	m := testManager(t, mustParse(t, oneResident(t)), ctl)
	st := statusFor(t, m, "web")
	if st.Health != HealthNotLoaded || !st.Drift {
		t.Errorf("want not-loaded+drift, got %+v", st)
	}
}

func TestStatusDeclaredOff(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    enabled: false
`)
	st := statusFor(t, testManager(t, cfg, newFakeController()), "web")
	if st.Health != HealthDeclaredOff || st.Drift {
		t.Errorf("want declared-off, no drift, got %+v", st)
	}
}

func TestStatusScheduledIdle(t *testing.T) {
	cfg := mustParse(t, `
services:
  job:
    type: scheduled
    command: /usr/bin/true
    schedule:
      interval: 6h
`)
	ctl := newFakeController()
	// A scheduled service that is loaded but waiting (no live pid).
	ctl.loaded["keep.job"] = launchd.PrintInfo{Loaded: true, State: "waiting"}
	st := statusFor(t, testManager(t, cfg, ctl), "job")
	if st.Health != HealthIdle {
		t.Errorf("Health = %q, want idle", st.Health)
	}
}

func TestStatusUnknownService(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newFakeController())
	if _, err := m.Status([]string{"nope"}); err == nil {
		t.Error("expected error for unknown service")
	}
}
