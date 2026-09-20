package keep

import (
	"errors"
	"os"
	"strings"
	"testing"
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
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)

	res, err := m.Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Added) != 1 || res.Added[0] != "web" {
		t.Fatalf("Added = %v, want [web]", res.Added)
	}
	data, err := os.ReadFile(artifactPath(t, m, &cfg.Services[0]))
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if !rt.ReadMarkers(artifactPath(t, m, &cfg.Services[0]), data).Managed {
		t.Error("written artifact is not marked managed")
	}
	if _, ok := rt.units["keep.web"]; !ok {
		t.Error("service was not loaded")
	}
}

func TestApplyIdempotent(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)

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
	rt := newTestRuntime(t)
	// Simulate a prior `keep down`: persistently disabled, not loaded.
	rt.held["keep.web"] = true
	m := testManager(t, cfg, rt)

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
	if _, ok := rt.units["keep.web"]; ok {
		t.Error("apply resurrected a held service (KeepAlive bug must not regress)")
	}
	if rt.didCall("load ") {
		t.Error("apply must not load a held service")
	}
	// The plist is still kept current on disk.
	if _, err := os.Stat(artifactPath(t, m, &cfg.Services[0])); err != nil {
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
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)

	res, err := m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DeclaredOff) != 1 || res.DeclaredOff[0] != "web" {
		t.Errorf("DeclaredOff = %v, want [web]", res.DeclaredOff)
	}
	if !rt.held["keep.web"] {
		t.Error("declared-off service should be held by the runtime")
	}
	if _, ok := rt.units["keep.web"]; ok {
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
	rt := newTestRuntime(t)
	rt.fail["hold keep.web"] = errors.New("hold failed")
	m := testManager(t, cfg, rt)

	_, err := m.Apply()
	if err == nil || !strings.Contains(err.Error(), "hold failed") {
		t.Fatalf("expected hold error, got %v", err)
	}
}

func TestApplyUpdatePropagatesBootoutError(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath(t, m, &cfg.Services[0]), handEdited("keep.web", "web"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.fail["unload keep.web"] = errors.New("unload failed")

	_, err := m.Apply()
	if err == nil || !strings.Contains(err.Error(), "unload failed") {
		t.Fatalf("expected unload error, got %v", err)
	}
}

func TestApplyPrunesOrphanOnlyIfManaged(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)

	// A managed artifact for a service no longer in the Config.
	orphan := fakeArtifact("keep.old", "old", "/opt/keep/bin/keep", "argv=/opt/keep/bin/keep fork old\n")
	orphanPath := m.ArtifactDir() + "/keep.old.unit"
	if err := os.WriteFile(orphanPath, orphan, 0o644); err != nil {
		t.Fatal(err)
	}
	rt.running("keep.old", 999)

	// An unmanaged unit that must never be touched.
	unmanaged := []byte("[Unit]\nDescription=someone else's\n")
	unmanagedPath := m.ArtifactDir() + "/com.vendor.thing.unit"
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
		t.Error("orphan artifact should have been pruned")
	}
	if _, ok := rt.units["keep.old"]; ok {
		t.Error("orphan should have been stopped")
	}
	if _, err := os.Stat(unmanagedPath); err != nil {
		t.Error("unmanaged artifact must never be touched")
	}
}
