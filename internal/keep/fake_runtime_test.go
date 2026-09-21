package keep

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/runtime"
)

// fakeRuntime is the in-memory runtime adapter. It models a service runtime's
// hold database and tracked-unit set closely enough to exercise the reconcile,
// status, doctor, and verb logic through the Manager's interface — no live
// machine, no launchctl. This is the second adapter that makes the runtime seam
// a real one rather than indirection, and it deliberately renders its own
// artifact format rather than plists: nothing above the seam may assume
// launchd's (ADR-0008).
type fakeRuntime struct {
	dir     string                  // artifact directory
	perUnit int                     // artifacts rendered per Service
	held    map[string]bool         // label -> persistently held
	units   map[string]runtime.Info // label -> live state (present == tracked)
	fail    map[string]error        // operation string -> injected failure
	heldErr error                   // injected failure for Held
	nextPID int
	calls   []string // ordered call log for assertions
}

func newFakeRuntime(dir string) *fakeRuntime {
	return &fakeRuntime{
		dir:     dir,
		perUnit: 1,
		held:    map[string]bool{},
		units:   map[string]runtime.Info{},
		fail:    map[string]error{},
		nextPID: 1000,
	}
}

func (f *fakeRuntime) ArtifactDir() string { return f.dir }

// unitSuffixes names the files this adapter emits per Service. The second one
// stands in for a runtime that needs more than one file (systemd's .timer).
func (f *fakeRuntime) unitSuffixes() []string {
	if f.perUnit > 1 {
		return []string{".unit", ".timer"}
	}
	return []string{".unit"}
}

func (f *fakeRuntime) Render(j runtime.Job) []runtime.Artifact {
	var arts []runtime.Artifact
	for _, suffix := range f.unitSuffixes() {
		body := fmt.Sprintf("argv=%s\nrunatload=%v\nkeepalive=%v\nstdout=%s\nstderr=%s\ninterval=%d\ncalendar=%d\n",
			strings.Join(j.ProgramArguments, " "), j.RunAtLoad, j.KeepAlive,
			j.StandardOutPath, j.StandardErrorPath, j.StartInterval, len(j.StartCalendar))
		arts = append(arts, runtime.Artifact{
			Path: filepath.Join(f.dir, j.Label+suffix),
			Data: fakeArtifact(j.Label, j.Service, j.KeepPath, body),
		})
	}
	return arts
}

// fakeArtifact renders this adapter's marked file format. Like a plist, the
// markers are a header the body cannot dislodge, so a hand-edited artifact is
// still recognizably keep's.
func fakeArtifact(label, service, keepPath, body string) []byte {
	return []byte(fmt.Sprintf("#keep-managed\nlabel=%s\nservice=%s\nkeeppath=%s\n%s",
		label, service, keepPath, body))
}

// handEdited is an artifact that still carries keep's markers but no longer
// matches what keep would render: the drift `apply` and `doctor` must catch.
func handEdited(label, service string) []byte {
	return fakeArtifact(label, service, "/opt/keep/bin/keep", "argv=/usr/bin/something-else\n")
}

func (f *fakeRuntime) ReadMarkers(path string, data []byte) runtime.MarkerInfo {
	ext := filepath.Ext(path)
	if ext != ".unit" && ext != ".timer" {
		return runtime.MarkerInfo{}
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || lines[0] != "#keep-managed" {
		return runtime.MarkerInfo{}
	}
	m := runtime.MarkerInfo{Managed: true}
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "label":
			m.Label = value
		case "service":
			m.Service = value
		case "keeppath":
			m.KeepPath = value
		}
	}
	return m
}

func (f *fakeRuntime) record(op, label string) error {
	f.calls = append(f.calls, op+" "+label)
	return f.fail[op+" "+label]
}

func (f *fakeRuntime) Load(_ context.Context, t runtime.Target) error {
	if err := f.record("load", t.Label); err != nil {
		return err
	}
	if f.held[t.Label] {
		return nil // a held unit is tracked by nobody until it is released
	}
	// Resident units start on load; scheduled ones wait for their next fire.
	info := runtime.Info{State: runtime.StateStopped}
	if !t.Scheduled {
		f.nextPID++
		info = runtime.Info{State: runtime.StateRunning, PID: f.nextPID}
	}
	f.units[t.Label] = info
	return nil
}

func (f *fakeRuntime) Unload(_ context.Context, t runtime.Target) error {
	if err := f.record("unload", t.Label); err != nil {
		return err
	}
	delete(f.units, t.Label)
	return nil
}

func (f *fakeRuntime) Hold(t runtime.Target) error {
	if err := f.record("hold", t.Label); err != nil {
		return err
	}
	f.held[t.Label] = true
	return nil
}

func (f *fakeRuntime) Release(t runtime.Target) error {
	if err := f.record("release", t.Label); err != nil {
		return err
	}
	f.held[t.Label] = false
	return nil
}

func (f *fakeRuntime) Start(t runtime.Target) error { return f.kick("start", t.Label) }

func (f *fakeRuntime) Restart(t runtime.Target) error { return f.kick("restart", t.Label) }

func (f *fakeRuntime) kick(op, label string) error {
	if err := f.record(op, label); err != nil {
		return err
	}
	if _, ok := f.units[label]; !ok {
		return fmt.Errorf("%s %s: not loaded", op, label)
	}
	f.nextPID++
	f.units[label] = runtime.Info{State: runtime.StateRunning, PID: f.nextPID}
	return nil
}

// Forget honours the artifact scoping the way a real adapter must: forgetting
// only a secondary artifact leaves the label's live unit alone, because that
// unit may have been taken over by another Service.
func (f *fakeRuntime) Forget(ctx context.Context, label string, paths []string) error {
	if err := f.record("forget", label); err != nil {
		return err
	}
	if !forgetsPrimaryUnit(label, paths) {
		return nil
	}
	delete(f.units, label)
	delete(f.held, label)
	return nil
}

// An empty path list means the whole label; otherwise only the ".unit" file
// stands for the label's live unit.
func forgetsPrimaryUnit(label string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if filepath.Base(p) == label+".unit" {
			return true
		}
	}
	return false
}

func (f *fakeRuntime) Info(t runtime.Target) (runtime.Info, error) {
	if info, ok := f.units[t.Label]; ok {
		return info, nil
	}
	return runtime.Info{State: runtime.StateUnknown}, nil
}

func (f *fakeRuntime) Held() (map[string]bool, error) {
	if f.heldErr != nil {
		return nil, f.heldErr
	}
	out := map[string]bool{}
	for k, v := range f.held {
		if v {
			out[k] = true
		}
	}
	return out, nil
}

func (f *fakeRuntime) Uptime(int) string { return "" }

func (f *fakeRuntime) didCall(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// diagnosingRuntime is a fake that also implements runtime.Diagnoser, the way
// the systemd adapter does. The plain fakeRuntime deliberately does not, so
// both sides of doctor's optional-capability check are exercised.
type diagnosingRuntime struct {
	*fakeRuntime
	diags []runtime.Diagnosis
}

func (d diagnosingRuntime) Diagnose() []runtime.Diagnosis { return d.diags }

// running marks a label as live without going through Load, for tests that
// need a specific PID or a state keep itself would not produce.
func (f *fakeRuntime) running(label string, pid int) {
	f.units[label] = runtime.Info{State: runtime.StateRunning, PID: pid}
}

// testManager builds a Manager wired to a fake runtime and temp artifact and
// state directories, so apply writes real files but drives a fake service
// runtime and never touches the real home directory.
func testManager(t *testing.T, cfg *config.Config, rt runtime.Runtime) *Manager {
	t.Helper()
	return &Manager{
		Cfg:      cfg,
		KeepPath: "/opt/keep/bin/keep",
		Version:  "test",
		rt:       rt,
		stateDir: t.TempDir(),
	}
}

// artifactPath is the on-disk path of a Service's first (often only) artifact.
func artifactPath(t *testing.T, m *Manager, s *config.Service) string {
	t.Helper()
	arts, err := m.Artifacts(s)
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	if len(arts) == 0 {
		t.Fatal("no artifacts rendered")
	}
	return arts[0].Path
}

// newTestRuntime returns a fake runtime rooted in a fresh temp directory.
func newTestRuntime(t *testing.T) *fakeRuntime {
	t.Helper()
	return newFakeRuntime(t.TempDir())
}

func mustParse(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return cfg
}
