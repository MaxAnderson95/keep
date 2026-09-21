package keep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

// The artifacts are keep's only record that an orphan exists. If they were
// deleted and Forget then failed, the next apply would find nothing to prune
// and report success while the Service kept running.
func TestApplyRestoresOrphanArtifactsWhenForgetFails(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	orphanPath := artifactPath(t, m, &cfg.Services[0])

	// Drop the Service from the Config, then fail the runtime's cleanup.
	m.Cfg.Services = m.Cfg.Services[:0]
	rt.fail["forget keep.web"] = errors.New("bootout failed")

	res, err := m.Apply()
	if err == nil || !strings.Contains(err.Error(), "bootout failed") {
		t.Fatalf("expected the forget error, got %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want none: nothing was successfully removed", res.Removed)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("artifact must be restored so the next apply can retry: %v", err)
	}

	// With the runtime healthy again, the retry finds and prunes the orphan.
	delete(rt.fail, "forget keep.web")
	res, err = m.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "web" {
		t.Fatalf("retry Removed = %v, want [web]", res.Removed)
	}
	if _, ok := rt.units["keep.web"]; ok {
		t.Error("the orphan should be stopped on the retry")
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("artifact should be gone after the successful retry")
	}
}

// The artifact directory is shared with whatever else the user keeps there.
// Reading a FIFO with no writer blocks forever, so the scan must not open
// anything that is not a regular file.
func TestScanSkipsSpecialFiles(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if err := syscall.Mkfifo(filepath.Join(m.ArtifactDir(), "unrelated.pipe"), 0o644); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := m.ComputePlan()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ComputePlan: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ComputePlan blocked on a FIFO in the artifact directory")
	}
}

// A symlink to a real artifact is still a real artifact.
func TestScanFollowsSymlinkedArtifacts(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	m := testManager(t, cfg, newTestRuntime(t))
	target := filepath.Join(t.TempDir(), "elsewhere.unit")
	if err := os.WriteFile(target, fakeArtifact("keep.linked", "linked", "/opt/keep/bin/keep", ""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(m.ArtifactDir(), "keep.linked.unit")); err != nil {
		t.Fatal(err)
	}

	managed, err := m.ScanManaged()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range managed {
		if a.Service == "linked" {
			return
		}
	}
	t.Fatalf("symlinked artifact not found in scan: %+v", managed)
}

// ~/Library/LaunchAgents always exists, but a Linux machine that has never had
// a user unit has no ~/.config/systemd/user. Somebody has to create it, and
// adapters never touch the filesystem.
func TestApplyCreatesTheArtifactDirectory(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newFakeRuntime(filepath.Join(t.TempDir(), "units"))
	m := testManager(t, cfg, rt)

	if _, err := m.Apply(); err != nil {
		t.Fatalf("apply into a missing artifact directory: %v", err)
	}
	if _, err := os.Stat(artifactPath(t, m, &cfg.Services[0])); err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
}

// A scheduled Service that becomes resident stops rendering its timer, but the
// timer is still on disk, still enabled, and still starts the Service — even
// after `keep down`. Nothing else reconciles it: the Service is still in the
// Config, so it is not an orphan.
func TestApplyRetiresArtifactsAServiceNoLongerRenders(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	rt.perUnit = 2 // the Service's old shape needed two files
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(rt.dir, "keep.web.timer")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("setup: the two-artifact shape should have written %s: %v", stale, err)
	}

	// The Service's shape changes: one artifact from here on.
	rt.perUnit = 1

	// diff has to say apply will delete it.
	sp := planFor(t, m, "web")
	if sp.Kind == ChangeNoop {
		t.Error("a Service with a retired artifact is not a noop")
	}
	if !strings.Contains(sp.Reason, "keep.web.timer") {
		t.Errorf("Reason = %q, should name the retired file", sp.Reason)
	}

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("%s should have been retired", stale)
	}
	if !rt.didCall("forget keep.web") {
		t.Errorf("the runtime should have been told to forget the retired unit: %v", rt.calls)
	}
	// The Service itself survives its own reshaping.
	if _, err := os.Stat(artifactPath(t, m, &cfg.Services[0])); err != nil {
		t.Errorf("the Service's current artifact should still be there: %v", err)
	}
}

// Retiring uses Forget, which clears a label's persistent records — including,
// on some runtimes, the hold. A Service the user Down'd must not come back
// just because its shape changed.
func TestApplyRetireKeepsAHold(t *testing.T) {
	cfg := mustParse(t, oneResident(t))
	rt := newTestRuntime(t)
	rt.perUnit = 2
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(context.Background(), &cfg.Services[0]); err != nil {
		t.Fatal(err)
	}

	rt.perUnit = 1
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}

	held, err := rt.Held()
	if err != nil {
		t.Fatal(err)
	}
	if !held["keep.web"] {
		t.Error("apply lost the hold while retiring a stale artifact")
	}
	if _, ok := rt.units["keep.web"]; ok {
		t.Error("a held Service must not be running after apply")
	}
	if _, err := os.Stat(filepath.Join(rt.dir, "keep.web.timer")); !os.IsNotExist(err) {
		t.Error("the stale artifact should be retired even for a held Service")
	}
}

// Two Services swapping labels hand their artifacts over. Neither file is
// stale: each is claimed by the other Service in the same apply, and deleting
// one would take out a unit that is supposed to keep running.
func TestApplyDoesNotRetireAnArtifactAnotherServiceClaims(t *testing.T) {
	cfg := mustParse(t, `
services:
  a:
    command: /usr/bin/true
    label: keep.one
  b:
    command: /usr/bin/true
    label: keep.two
`)
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}

	// Swap the labels.
	for i := range m.Cfg.Services {
		switch m.Cfg.Services[i].Name {
		case "a":
			m.Cfg.Services[i].Label = "keep.two"
		case "b":
			m.Cfg.Services[i].Label = "keep.one"
		}
	}

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"keep.one", "keep.two"} {
		if _, err := os.Stat(filepath.Join(rt.dir, label+".unit")); err != nil {
			t.Errorf("%s should still exist after the swap: %v", label, err)
		}
		if _, ok := rt.units[label]; !ok {
			t.Errorf("%s should still be loaded after the swap", label)
		}
	}
	if rt.didCall("forget") {
		t.Errorf("nothing was abandoned, so nothing should be forgotten: %v", rt.calls)
	}
}

// A label the Config genuinely abandons is still retired.
func TestApplyRetiresAnAbandonedLabel(t *testing.T) {
	cfg := mustParse(t, `
services:
  a:
    command: /usr/bin/true
    label: keep.old
`)
	rt := newTestRuntime(t)
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	m.Cfg.Services[0].Label = "keep.new"

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rt.dir, "keep.old.unit")); !os.IsNotExist(err) {
		t.Error("the abandoned label's artifact should have been retired")
	}
	if !rt.didCall("forget keep.old") {
		t.Errorf("the abandoned label should have been forgotten: %v", rt.calls)
	}
	if _, ok := rt.units["keep.new"]; !ok {
		t.Error("the Service should be running under its new label")
	}
}

// The mixed swap: a scheduled Service and a resident one trade labels. The
// scheduled Service's old timer is genuinely abandoned, but its old .service
// path now belongs to the resident Service, so retiring the timer must not
// take the whole label down with it.
func TestApplyRetiresOneArtifactWithoutStoppingItsSurvivingSibling(t *testing.T) {
	cfg := mustParse(t, `
services:
  asrv:
    command: /usr/bin/true
    label: keep.two
  zjob:
    type: scheduled
    command: /usr/bin/true
    label: keep.one
    schedule:
      interval: 1h
`)
	rt := newTestRuntime(t)
	rt.perUnit = 2 // the scheduled Service renders two files
	m := testManager(t, cfg, rt)
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}

	// Services reconcile in name order, so the resident Service takes the
	// label over before the scheduled one retires its leftover timer: the
	// retirement has to leave a unit alone that is already live.
	// Swap the labels, and let the resident Service render a single file.
	rt.perUnit = 1
	for i := range m.Cfg.Services {
		switch m.Cfg.Services[i].Name {
		case "zjob":
			m.Cfg.Services[i].Label = "keep.two"
		case "asrv":
			m.Cfg.Services[i].Label = "keep.one"
		}
	}
	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}

	// The abandoned timer is gone.
	if _, err := os.Stat(filepath.Join(rt.dir, "keep.one.timer")); !os.IsNotExist(err) {
		t.Error("the abandoned timer should have been retired")
	}
	// The label's other artifact was taken over, not abandoned.
	if _, err := os.Stat(filepath.Join(rt.dir, "keep.one.unit")); err != nil {
		t.Errorf("keep.one.unit now belongs to another Service: %v", err)
	}
	if _, ok := rt.units["keep.one"]; !ok {
		t.Error("retiring a sibling artifact stopped the unit that took the label over")
	}
}
