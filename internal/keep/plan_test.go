package keep

import (
	"os"
	"testing"
)

// planFor returns the ServicePlan for a single named service.
func planFor(t *testing.T, m *Manager, name string) ServicePlan {
	t.Helper()
	plan, err := m.ComputePlan()
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	for _, sp := range plan.Services {
		if sp.Name == name {
			return sp
		}
	}
	t.Fatalf("no plan entry for %q", name)
	return ServicePlan{}
}

func TestPlanAdd(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newFakeController())
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeAdd {
		t.Errorf("Kind = %q, want add", sp.Kind)
	}
}

func TestPlanNoopAfterApply(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeNoop {
		t.Errorf("Kind = %q, want noop", sp.Kind)
	}
}

func TestPlanUpdateOnHandEdit(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	// Hand-edit the generated artifact.
	path := m.PlistPath(&m.Cfg.Services[0])
	if err := os.WriteFile(path, []byte("tampered but still KeepManaged <key>KeepManaged</key><true/> <key>KeepService</key><string>web</string>"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeUpdate {
		t.Errorf("Kind = %q, want update (hand-edit detected)", sp.Kind)
	}
}

func TestPlanHeld(t *testing.T) {
	ctl := newFakeController()
	ctl.disabled["keep.web"] = true
	m := testManager(t, mustParse(t, oneResident(t)), ctl)
	sp := planFor(t, m, "web")
	if !sp.Held {
		t.Error("expected Held (config enabled, launchd disabled)")
	}
}

func TestPlanDeclaredOffAndDrift(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    enabled: false
`)
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	// First apply makes it declared-off (disabled, not drift).
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if !sp.DeclaredOff || sp.DisabledDrift {
		t.Errorf("want DeclaredOff && !DisabledDrift, got %+v", sp)
	}
	// Now simulate someone enabling it behind keep's back: declared off but live-enabled = drift.
	ctl.disabled["keep.web"] = false
	sp = planFor(t, m, "web")
	if !sp.DisabledDrift {
		t.Errorf("want DisabledDrift (declared off but live-enabled), got %+v", sp)
	}
}

func TestPlanOrphanRemove(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	// Drop web from the Config and re-plan: it becomes an orphan to remove.
	m.Cfg.Services = m.Cfg.Services[:0]
	plan, err := m.ComputePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Removes) != 1 || plan.Removes[0].Name != "web" {
		t.Errorf("Removes = %v, want [web]", plan.Removes)
	}
}
