package keep

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/launchd"
)

// fakeController is the in-memory Controller adapter. It models launchd's
// enable/disable database and bootstrapped set closely enough to exercise the
// reconcile, status, doctor, and verb logic through the Manager's interface —
// no live machine, no launchctl. This is the second adapter that makes the
// launchd seam a real one rather than indirection.
type fakeController struct {
	disabled map[string]bool              // label -> persistently disabled
	loaded   map[string]launchd.PrintInfo // label -> live state (present == bootstrapped)
	fail     map[string]error             // operation string -> injected failure
	nextPID  int
	calls    []string // ordered call log for assertions
}

func newFakeController() *fakeController {
	return &fakeController{
		disabled: map[string]bool{},
		loaded:   map[string]launchd.PrintInfo{},
		fail:     map[string]error{},
		nextPID:  1000,
	}
}

func labelFromPlist(plistPath string) string {
	return strings.TrimSuffix(filepath.Base(plistPath), ".plist")
}

func (f *fakeController) Bootstrap(plistPath string) error {
	label := labelFromPlist(plistPath)
	f.calls = append(f.calls, "bootstrap "+label)
	if err := f.fail["bootstrap "+label]; err != nil {
		return err
	}
	if f.disabled[label] {
		return nil // launchd will not run a disabled service
	}
	f.nextPID++
	f.loaded[label] = launchd.PrintInfo{Loaded: true, State: "running", PID: f.nextPID, HasPID: true}
	return nil
}

func (f *fakeController) Bootout(label string) error {
	f.calls = append(f.calls, "bootout "+label)
	if err := f.fail["bootout "+label]; err != nil {
		return err
	}
	delete(f.loaded, label)
	return nil
}

func (f *fakeController) Enable(label string) error {
	f.calls = append(f.calls, "enable "+label)
	if err := f.fail["enable "+label]; err != nil {
		return err
	}
	f.disabled[label] = false
	return nil
}

func (f *fakeController) Disable(label string) error {
	f.calls = append(f.calls, "disable "+label)
	if err := f.fail["disable "+label]; err != nil {
		return err
	}
	f.disabled[label] = true
	return nil
}

func (f *fakeController) Kickstart(label string, kill bool) error {
	f.calls = append(f.calls, fmt.Sprintf("kickstart kill=%v %s", kill, label))
	if err := f.fail["kickstart "+label]; err != nil {
		return err
	}
	info, ok := f.loaded[label]
	if !ok {
		return fmt.Errorf("kickstart %s: not loaded", label)
	}
	f.nextPID++
	info.PID = f.nextPID
	info.HasPID = true
	info.State = "running"
	f.loaded[label] = info
	return nil
}

func (f *fakeController) Info(label string) (launchd.PrintInfo, error) {
	if info, ok := f.loaded[label]; ok {
		return info, nil
	}
	return launchd.PrintInfo{Loaded: false}, nil
}

func (f *fakeController) DisabledSet() (map[string]bool, error) {
	out := map[string]bool{}
	for k, v := range f.disabled {
		if v {
			out[k] = true
		}
	}
	return out, nil
}

func (f *fakeController) Uptime(pid int) string { return "" }

func (f *fakeController) didCall(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// testManager builds a Manager wired to a fake Controller and a temp agents
// directory, so apply writes real files but drives a fake launchd.
func testManager(t *testing.T, cfg *config.Config, ctl launchd.Controller) *Manager {
	t.Helper()
	return &Manager{
		Cfg:       cfg,
		KeepPath:  "/opt/keep/bin/keep",
		Version:   "test",
		ctl:       ctl,
		agentsDir: t.TempDir(),
	}
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
