package keep

import (
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
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
	rt := newTestRuntime(t)
	rt.running("keep.web", 4242)
	m := testManager(t, mustParse(t, oneResident(t)), rt)
	st := statusFor(t, m, "web")
	if st.Health != HealthRunning {
		t.Errorf("Health = %q, want running", st.Health)
	}
	if st.PID != 4242 || st.Drift {
		t.Errorf("unexpected status %+v", st)
	}
}

func TestStatusHeld(t *testing.T) {
	rt := newTestRuntime(t)
	rt.held["keep.web"] = true
	m := testManager(t, mustParse(t, oneResident(t)), rt)
	st := statusFor(t, m, "web")
	if st.Health != HealthHeld || !st.Drift || !st.Held {
		t.Errorf("want held+drift, got %+v", st)
	}
}

func TestStatusNotLoadedIsDrift(t *testing.T) {
	rt := newTestRuntime(t) // enabled, not disabled, not loaded
	m := testManager(t, mustParse(t, oneResident(t)), rt)
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
	st := statusFor(t, testManager(t, cfg, newTestRuntime(t)), "web")
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
	rt := newTestRuntime(t)
	// A scheduled service that is loaded but waiting (no live pid).
	rt.units["keep.job"] = runtime.Info{State: runtime.StateStopped}
	st := statusFor(t, testManager(t, cfg, rt), "job")
	if st.Health != HealthIdle {
		t.Errorf("Health = %q, want idle", st.Health)
	}
}

func TestStatusUnknownService(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newTestRuntime(t))
	if _, err := m.Status([]string{"nope"}); err == nil {
		t.Error("expected error for unknown service")
	}
}
