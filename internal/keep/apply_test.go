package keep

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/launchd"
)

func oneResident(t *testing.T) string {
	return `
services:
  web:
    command: /usr/bin/true
`
}

func TestApplyAddBootstrapsAndWritesPlist(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)

	res, err := m.Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Added) != 1 || res.Added[0] != "web" {
		t.Fatalf("Added = %v, want [web]", res.Added)
	}
	data, err := os.ReadFile(m.PlistPath(&cfg.Services[0]))
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if !launchd.IsManaged(data) {
		t.Error("written plist is not marked managed")
	}
	if _, ok := ctl.loaded["keep.web"]; !ok {
		t.Error("service was not bootstrapped")
	}
}

func TestApplyIdempotent(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 0 {
		t.Errorf("second apply Added = %v, want none", res.Added)
	}
	if len(res.Unchanged) != 1 || res.Unchanged[0] != "web" {
		t.Errorf("second apply Unchanged = %v, want [web]", res.Unchanged)
	}
}

func TestApplyRespectsHold(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	// Simulate a prior `keep down`: persistently disabled, not loaded.
	ctl.disabled["keep.web"] = true
	m := testManager(t, cfg, ctl)

	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Held) != 1 || res.Held[0] != "web" {
		t.Errorf("Held = %v, want [web]", res.Held)
	}
	if len(res.Added) != 0 {
		t.Errorf("a held service must not be added/started, got Added=%v", res.Added)
	}
	if _, ok := ctl.loaded["keep.web"]; ok {
		t.Error("apply resurrected a held service (KeepAlive bug must not regress)")
	}
	if ctl.didCall("bootstrap") {
		t.Error("apply must not bootstrap a held service")
	}
	// The plist is still kept current on disk.
	if _, err := os.Stat(m.PlistPath(&cfg.Services[0])); err != nil {
		t.Errorf("held service plist should still be written: %v", err)
	}
}

func TestApplyDeclaredOff(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    enabled: false
`)
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)

	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DeclaredOff) != 1 || res.DeclaredOff[0] != "web" {
		t.Errorf("DeclaredOff = %v, want [web]", res.DeclaredOff)
	}
	if !ctl.disabled["keep.web"] {
		t.Error("declared-off service should be disabled in launchd")
	}
	if _, ok := ctl.loaded["keep.web"]; ok {
		t.Error("declared-off service should not be loaded")
	}
}

func TestApplyDeclaredOffPropagatesDisableError(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    enabled: false
`)
	ctl := newFakeController()
	ctl.fail["disable keep.web"] = errors.New("disable failed")
	m := testManager(t, cfg, ctl)

	_, err := m.Apply()
	if err == nil || !strings.Contains(err.Error(), "disable failed") {
		t.Fatalf("expected disable error, got %v", err)
	}
}

func TestApplyUpdatePropagatesBootoutError(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.PlistPath(&cfg.Services[0]), []byte("tampered <key>KeepManaged</key><true/> <key>KeepService</key><string>web</string>"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctl.fail["bootout keep.web"] = errors.New("bootout failed")

	_, err := m.Apply()
	if err == nil || !strings.Contains(err.Error(), "bootout failed") {
		t.Fatalf("expected bootout error, got %v", err)
	}
}

func TestApplyPrunesOrphanOnlyIfManaged(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)

	// A managed artifact for a service no longer in the Config.
	orphan := launchd.Render(launchd.Job{
		Label:            "keep.old",
		ProgramArguments: []string{"/opt/keep/bin/keep", "fork", "old"},
		RunAtLoad:        true,
		KeepAlive:        true,
		Service:          "old",
	})
	orphanPath := m.LaunchAgentsDir() + "/keep.old.plist"
	if err := os.WriteFile(orphanPath, orphan, 0o644); err != nil {
		t.Fatal(err)
	}
	ctl.loaded["keep.old"] = launchd.PrintInfo{Loaded: true, State: "running", PID: 999, HasPID: true}

	// An unmanaged plist that must never be touched.
	unmanaged := []byte(`<?xml version="1.0"?><plist><dict><key>Label</key><string>com.vendor.thing</string></dict></plist>`)
	unmanagedPath := m.LaunchAgentsDir() + "/com.vendor.thing.plist"
	if err := os.WriteFile(unmanagedPath, unmanaged, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "old" {
		t.Errorf("Removed = %v, want [old]", res.Removed)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan plist should have been pruned")
	}
	if _, ok := ctl.loaded["keep.old"]; ok {
		t.Error("orphan should have been booted out")
	}
	if _, err := os.Stat(unmanagedPath); err != nil {
		t.Error("unmanaged plist must never be touched")
	}
}
