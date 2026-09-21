package keep

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/runtime"
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
	m := testManager(t, cfg, newTestRuntime(t))
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
	m := testManager(t, cfg, newTestRuntime(t))
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
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	orphan := fakeArtifact("keep.ghost", "ghost", "/opt/keep/bin/keep", "argv=/opt/keep/bin/keep fork ghost\n")
	if err := os.WriteFile(m.ArtifactDir()+"/keep.ghost.unit", orphan, 0o644); err != nil {
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
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	// Write an artifact pinned to a different keep path.
	stale := fakeArtifact("keep.web", "web", "/old/location/keep", "argv=/old/location/keep fork web\n")
	if err := os.WriteFile(artifactPath(t, m, &cfg.Services[0]), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	rt.running("keep.web", 5)
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
	m := testManager(t, cfg, newTestRuntime(t)) // enabled, not loaded
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	// Stop it behind keep's back.
	_ = m.rt.Unload(context.Background(), runtime.Target{Label: "keep.web"})
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
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	svc, _ := cfg.Service("web")
	return m, svc
}

func livePID(t *testing.T, m *Manager, svc *config.Service) int {
	t.Helper()
	info, err := m.rt.Info(m.target(svc))
	if err != nil || info.PID == 0 {
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
	m := testManager(t, cfg, newTestRuntime(t))
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
	m := testManager(t, cfg, newTestRuntime(t))
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

// A doctor that fails with a raw "cannot reach the service manager" error
// withholds the one finding that says how to fix exactly that.
func TestDoctorReportsRuntimeDiagnosisWhenTheRuntimeIsUnusable(t *testing.T) {
	fake := newTestRuntime(t)
	fake.heldErr = errors.New("Failed to connect to bus: No medium found")
	rt := diagnosingRuntime{fakeRuntime: fake, diags: []runtime.Diagnosis{{
		Severity: runtime.SevError,
		Problem:  "systemctl --user is not reachable",
		Fix:      "log in as this user, or enable lingering",
	}}}
	m := testManager(t, mustParse(t, oneResident(t)), rt)

	findings, err := m.Doctor()
	if err != nil {
		t.Fatalf("the diagnosis is the answer, not an error: %v", err)
	}
	if !findingsContain(findings, "not reachable") {
		t.Fatalf("expected the runtime diagnosis, got %+v", findings)
	}
	if findings[0].Severity != SevError {
		t.Errorf("severity = %q, want error", findings[0].Severity)
	}
}

// Without a diagnosis to offer, the underlying failure must still surface.
func TestDoctorStillFailsWhenTheRuntimeCannotExplainItself(t *testing.T) {
	fake := newTestRuntime(t)
	fake.heldErr = errors.New("something broke")
	m := testManager(t, mustParse(t, oneResident(t)), fake)

	if _, err := m.Doctor(); err == nil {
		t.Fatal("expected the runtime error to surface")
	}
}

// An adapter's environment findings come first: they explain every
// per-Service symptom under them.
func TestDoctorPutsRuntimeFindingsFirst(t *testing.T) {
	rt := diagnosingRuntime{fakeRuntime: newTestRuntime(t), diags: []runtime.Diagnosis{{
		Severity: runtime.SevWarning, Problem: "lingering is off", Fix: "enable-linger",
	}}}
	m := testManager(t, mustParse(t, oneResident(t)), rt)

	findings, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 || findings[0].Problem != "lingering is off" {
		t.Fatalf("runtime findings should lead, got %+v", findings)
	}
	if findings[0].Service != "" {
		t.Errorf("a runtime finding names no Service, got %q", findings[0].Service)
	}
}
