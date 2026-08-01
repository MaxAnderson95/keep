package keep

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
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
	m.ctl.(*fakeController).Bootout(context.Background(), "keep.web")
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "not loaded") {
		t.Errorf("expected not-loaded finding, got %+v", findings)
	}
}

// versionedResident is a running Service declaring a version_command whose
// binary exists, so only version-capture findings can fire.
func versionedResident(t *testing.T) (*Manager, *config.Service) {
	t.Helper()
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    version_command: /usr/bin/true --version
`)
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	svc, _ := cfg.Service("web")
	return m, svc
}

func livePID(t *testing.T, m *Manager, svc *config.Service) int {
	t.Helper()
	info, err := m.ctl.Info(svc.EffectiveLabel())
	if err != nil || !info.HasPID {
		t.Fatalf("no live pid: info=%+v err=%v", info, err)
	}
	return info.PID
}

// Declaring version_command changes no artifact, so apply never restarts
// anything to capture it. Doctor is what explains the blank.
func TestDoctorVersionNotCaptured(t *testing.T) {
	m, _ := versionedResident(t)
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "no version captured") {
		t.Fatalf("expected not-captured finding, got %+v", findings)
	}
	for _, f := range findings {
		if strings.Contains(f.Problem, "no version captured") && f.Severity != SevInfo {
			t.Errorf("severity = %q, want %q", f.Severity, SevInfo)
		}
	}
}

func TestDoctorVersionCaptureFailure(t *testing.T) {
	m, svc := versionedResident(t)
	if err := m.writeVersionEntry("web", VersionEntry{
		PID: livePID(t, m, svc), Command: svc.VersionCommand, Error: "exit status 127",
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "version_command failed at the last start") {
		t.Fatalf("expected capture-failure finding, got %+v", findings)
	}
}

func TestDoctorQuietWhenVersionCaptured(t *testing.T) {
	m, svc := versionedResident(t)
	if err := m.writeVersionEntry("web", VersionEntry{
		Version: "1.0.0", PID: livePID(t, m, svc), Command: svc.VersionCommand,
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected clean doctor, got %+v", findings)
	}
}

func TestDoctorVersionCommandBinaryMissing(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    version_command: /definitely/not/a/real/binary-xyz --version
`)
	m := testManager(t, cfg, newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContain(findings, "version_command binary") {
		t.Fatalf("expected version-binary finding, got %+v", findings)
	}
	if findingsContain(findings, "target binary") {
		t.Fatalf("the target binary exists; it must not be flagged: %+v", findings)
	}
}

// Optionality (D26): a Service declaring no version_command produces no
// version-related finding of any kind, including the info one.
func TestDoctorSilentWithoutVersionCommand(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	m := testManager(t, cfg, newFakeController())
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if findingsContain(findings, "version") {
		t.Fatalf("version finding for a Service that declares none: %+v", findings)
	}
}
