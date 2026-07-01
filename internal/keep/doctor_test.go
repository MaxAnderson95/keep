package keep

import (
	"os"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/launchd"
)

func findingsContain(fs []Finding, substr string) bool {
	for _, f := range fs {
		if strings.Contains(f.Problem, substr) {
			return true
		}
	}
	return false
}

func TestDoctorCleanAfterApply(t *testing.T) {
	cfg := mustParse(t, oneResident(t)) // command /usr/bin/true exists
	m := testManager(t, cfg, newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("expected clean doctor, got %+v", findings)
	}
}

func TestDoctorMissingBinary(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /definitely/not/a/real/binary-xyz
`)
	m := testManager(t, cfg, newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "target binary") {
		t.Errorf("expected missing-binary finding, got %+v", findings)
	}
}

func TestDoctorOrphan(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	orphan := launchd.Render(launchd.Job{
		Label:            "keep.ghost",
		ProgramArguments: []string{"/opt/keep/bin/keep", "fork", "ghost"},
		Service:          "ghost",
	})
	if err := os.WriteFile(m.LaunchAgentsDir()+"/keep.ghost.plist", orphan, 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "orphaned managed artifact") {
		t.Errorf("expected orphan finding, got %+v", findings)
	}
}

func TestDoctorStaleKeepPath(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	// Write an artifact pinned to a different keep path.
	stale := launchd.Render(launchd.Job{
		Label:            "keep.web",
		ProgramArguments: []string{"/old/location/keep", "fork", "web"},
		RunAtLoad:        true,
		KeepAlive:        true,
		Service:          "web",
		KeepPath:         "/old/location/keep",
	})
	if err := os.WriteFile(m.PlistPath(&cfg.Services[0]), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	ctl.loaded["keep.web"] = launchd.PrintInfo{Loaded: true, State: "running", PID: 5, HasPID: true}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "stale keep path") {
		t.Errorf("expected stale-keep-path finding, got %+v", findings)
	}
}

func TestDoctorNotLoaded(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	m := testManager(t, cfg, newFakeController()) // enabled, not loaded
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	// Boot it out behind keep's back.
	m.ctl.(*fakeController).Bootout("keep.web")
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "not loaded") {
		t.Errorf("expected not-loaded finding, got %+v", findings)
	}
}
