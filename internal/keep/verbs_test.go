package keep

import (
	"context"
	"testing"
)

func TestDownPersistentlyHolds(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(context.Background(), &cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	if !rt.held["keep.web"] {
		t.Error("down must persistently hold the service")
	}
	if _, ok := rt.units["keep.web"]; ok {
		t.Error("down must boot the service out")
	}
}

func TestUpEnablesAndStarts(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	rt.held["keep.web"] = true // previously held
	m := testManager(t, cfg, rt)
	if err := m.Up(&cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	if rt.held["keep.web"] {
		t.Error("up must release the hold")
	}
	if _, ok := rt.units["keep.web"]; !ok {
		t.Error("up must load the service")
	}
}

func TestBounceRestartsInPlace(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	before := rt.units["keep.web"].PID
	if err := m.Bounce(&cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	after := rt.units["keep.web"].PID
	if before == after {
		t.Errorf("bounce should restart in place (pid %d -> %d)", before, after)
	}
	if !rt.didCall("restart keep.web") {
		t.Error("bounce must restart the unit")
	}
}

func TestVerbTargetsAllWhenEmpty(t *testing.T) {
	cfg := mustParse(t, `
services:
  a:
    command: /usr/bin/true
  b:
    command: /usr/bin/true
`)
	m := testManager(t, cfg, newTestRuntime(t))
	targets, err := m.Targets(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Errorf("Targets(nil) = %d services, want 2", len(targets))
	}
}
