package keep

import "testing"

func TestDownPersistentlyHolds(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(&cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	if !ctl.disabled["keep.web"] {
		t.Error("down must persistently disable the service")
	}
	if _, ok := ctl.loaded["keep.web"]; ok {
		t.Error("down must boot the service out")
	}
}

func TestUpEnablesAndStarts(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	ctl.disabled["keep.web"] = true // previously held
	m := testManager(t, cfg, ctl)
	if err := m.Up(&cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	if ctl.disabled["keep.web"] {
		t.Error("up must clear the disable")
	}
	if _, ok := ctl.loaded["keep.web"]; !ok {
		t.Error("up must bootstrap the service")
	}
}

func TestBounceRestartsInPlace(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	before := ctl.loaded["keep.web"].PID
	if err := m.Bounce(&cfg.Services[0]); err != nil {
		t.Fatal(err)
	}
	after := ctl.loaded["keep.web"].PID
	if before == after {
		t.Errorf("bounce should restart in place (pid %d -> %d)", before, after)
	}
	if !ctl.didCall("kickstart kill=true keep.web") {
		t.Error("bounce must kickstart -k")
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
	m := testManager(t, cfg, newFakeController())
	targets, err := m.Targets(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Errorf("Targets(nil) = %d services, want 2", len(targets))
	}
}
