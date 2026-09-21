package keep

import (
	"os"
	"strings"
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
	m := testManager(t, mustParse(t, oneResident(t)), newTestRuntime(t))
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeAdd {
		t.Errorf("Kind = %q, want add", sp.Kind)
	}
}

func TestPlanNoopAfterApply(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newTestRuntime(t))
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeNoop {
		t.Errorf("Kind = %q, want noop", sp.Kind)
	}
}

func TestPlanUpdateOnHandEdit(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newTestRuntime(t))
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	// Hand-edit the generated artifact.
	path := artifactPath(t, m, &m.Cfg.Services[0])
	if err := os.WriteFile(path, handEdited("keep.web", "web"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if sp.Kind != ChangeUpdate {
		t.Errorf("Kind = %q, want update (hand-edit detected)", sp.Kind)
	}
}

func TestPlanHeld(t *testing.T) {
	rt := newTestRuntime(t)
	rt.held["keep.web"] = true
	m := testManager(t, mustParse(t, oneResident(t)), rt)
	sp := planFor(t, m, "web")
	if !sp.Held {
		t.Error("expected Held (config enabled, runtime holding it down)")
	}
}

func TestPlanDeclaredOffAndDrift(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    enabled: false
`)
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	// First apply makes it declared-off (disabled, not drift).
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	sp := planFor(t, m, "web")
	if !sp.DeclaredOff || sp.DisabledDrift {
		t.Errorf("want DeclaredOff && !DisabledDrift, got %+v", sp)
	}
	// Now simulate someone enabling it behind keep's back: declared off but live-enabled = drift.
	rt.held["keep.web"] = false
	sp = planFor(t, m, "web")
	if !sp.DisabledDrift {
		t.Errorf("want DisabledDrift (declared off but live-enabled), got %+v", sp)
	}
}

func TestPlanOrphanRemove(t *testing.T) {
	m := testManager(t, mustParse(t, oneResident(t)), newTestRuntime(t))
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

// A runtime may need more than one file per Service (systemd wants a .timer
// alongside the .service). keep still plans, applies, and prunes the Service as
// one thing.
func TestMultiArtifactServiceIsPlannedAndPrunedAsOne(t *testing.T) {
	rt := newTestRuntime(t)
	rt.perUnit = 2
	m := testManager(t, mustParse(t, oneResident(t)), rt)

	sp := planFor(t, m, "web")
	if sp.Kind != ChangeAdd {
		t.Fatalf("Kind = %q, want add", sp.Kind)
	}
	for _, name := range []string{"keep.web.unit", "keep.web.timer"} {
		if !strings.Contains(sp.Reason, name) {
			t.Errorf("Reason %q should name the missing file %q", sp.Reason, name)
		}
	}

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	arts, err := m.Artifacts(&m.Cfg.Services[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("want 2 artifacts, got %d", len(arts))
	}
	for _, a := range arts {
		if _, err := os.Stat(a.Path); err != nil {
			t.Fatalf("artifact not written: %v", err)
		}
	}

	// Hand-edit one of the two: one Service entry, naming only the file that
	// actually drifted.
	if err := os.WriteFile(arts[1].Path, handEdited("keep.web", "web"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := m.ComputePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Services) != 1 {
		t.Fatalf("want 1 Service entry, got %d", len(plan.Services))
	}
	sp = plan.Services[0]
	if sp.Kind != ChangeUpdate {
		t.Errorf("Kind = %q, want update", sp.Kind)
	}
	if !strings.Contains(sp.Reason, "keep.web.timer") || strings.Contains(sp.Reason, "keep.web.unit") {
		t.Errorf("Reason = %q, want only the drifted file named", sp.Reason)
	}

	// Drop the Service: both files go, and the label is forgotten once.
	m.Cfg.Services = m.Cfg.Services[:0]
	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "web" {
		t.Fatalf("Removed = %v, want [web]", res.Removed)
	}
	for _, a := range arts {
		if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned", a.Path)
		}
	}
	var forgets int
	for _, c := range rt.calls {
		if c == "forget keep.web" {
			forgets++
		}
	}
	if forgets != 1 {
		t.Errorf("forget called %d times, want 1", forgets)
	}
}
