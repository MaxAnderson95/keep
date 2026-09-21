package launchd

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/runtime"
)

// Runtime is the launchd adapter: it renders plists into ~/Library/LaunchAgents
// and drives launchctl in the rootless per-user GUI domain (D25). It is the
// only place that knows launchd's mechanics; everything above the
// runtime.Runtime seam speaks in Services (ADR-0008).
type Runtime struct {
	// run executes launchctl; an internal seam so the adapter's own wiring is
	// not hard-bound to exec. Not exposed — keep's tests use an in-memory
	// adapter at the runtime.Runtime interface instead.
	run         func(args ...string) (string, error)
	artifactDir string
}

// New returns the launchctl-backed Runtime.
func New() *Runtime {
	return &Runtime{run: execLaunchctl, artifactDir: config.LaunchAgentsDir()}
}

func execLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// ArtifactDir is the user LaunchAgents directory launchd loads agents from.
func (r *Runtime) ArtifactDir() string { return r.artifactDir }

// Render produces the single plist launchd needs for a Job.
func (r *Runtime) Render(j runtime.Job) []runtime.Artifact {
	return []runtime.Artifact{{
		Path: r.plistPath(j.Label),
		Data: renderPlist(j),
	}}
}

// ReadMarkers parses keep's markers out of a LaunchAgents file. Anything that
// is not a plist is not something this adapter generated.
func (r *Runtime) ReadMarkers(path string, data []byte) runtime.MarkerInfo {
	if filepath.Ext(path) != ".plist" {
		return runtime.MarkerInfo{}
	}
	return readMarkers(data)
}

func (r *Runtime) plistPath(label string) string {
	return filepath.Join(r.ArtifactDir(), label+".plist")
}

// Load bootstraps the label's plist into the GUI domain (idempotent: an
// already-loaded service is treated as success). Resident jobs carry RunAtLoad
// and start here; scheduled jobs wait for their next fire.
//
// launchd reads the artifact off disk, so the Manager must have written it
// first. ctx is unused: bootstrap does not wait on the service.
func (r *Runtime) Load(_ context.Context, t runtime.Target) error {
	path := r.plistPath(t.Label)
	out, err := r.run("bootstrap", Domain(), path)
	if err != nil {
		if strings.Contains(out, "already bootstrapped") || strings.Contains(out, "service already loaded") {
			return nil
		}
		return fmt.Errorf("bootstrap %s: %s", path, strings.TrimSpace(out))
	}
	return nil
}

// bootoutSettleTimeout bounds how long Unload waits for launchd to finish a
// teardown, and bootoutPollInterval is how often it re-checks. launchd itself
// SIGKILLs a job that overruns its ExitTimeOut (20s unless the plist says
// otherwise), so the ceiling only trips when launchd is wedged. Variables for
// tests.
var (
	bootoutSettleTimeout = 30 * time.Second
	bootoutPollInterval  = 50 * time.Millisecond
)

// Unload boots the label out of the domain and waits for the teardown to
// finish (idempotent: not-loaded is success).
//
// `launchctl bootout` returns once it has signalled the job, not once the job
// is gone: a service that drains on SIGTERM stays in the domain in state
// "SIGTERMed" until it exits. Bootstrapping that label inside the window fails
// with "5: Input/output error", so every caller that stops a service in order
// to start it again — Down/Up around `update`, reloadService in `apply` — would
// race launchd and leave the Service down. Settling here is what makes Unload
// mean what its name says for all of them.
//
// ctx bounds the wait, not the unload: by the time it can be canceled launchd
// has already been told to stop the service, so cancellation abandons the
// watch rather than undoing anything.
func (r *Runtime) Unload(ctx context.Context, t runtime.Target) error {
	return r.unloadLabel(ctx, t.Label)
}

func (r *Runtime) unloadLabel(ctx context.Context, label string) error {
	out, err := r.run("bootout", Target(label))
	if err != nil {
		if strings.Contains(out, "No such process") || strings.Contains(out, "Could not find") {
			return nil
		}
		return fmt.Errorf("bootout %s: %s", label, strings.TrimSpace(out))
	}
	return r.waitBootedOut(ctx, label)
}

// waitBootedOut polls until the label is no longer in the domain. An
// unreadable domain ends the wait rather than failing it: the caller asked to
// stop a service, and a broken `print` is no reason to report that the stop
// failed.
func (r *Runtime) waitBootedOut(ctx context.Context, label string) error {
	deadline := time.Now().Add(bootoutSettleTimeout)
	for {
		info, err := r.printInfo(label)
		if err != nil || !info.Loaded {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("bootout %s: still tearing down after %s (state %q)",
				label, bootoutSettleTimeout, info.State)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bootout %s: %w (state %q)", label, ctx.Err(), info.State)
		case <-time.After(bootoutPollInterval):
		}
	}
}

// Release clears any persistent disable for the label (reverses Hold).
func (r *Runtime) Release(t runtime.Target) error { return r.enable(t.Label) }

func (r *Runtime) enable(label string) error {
	out, err := r.run("enable", Target(label))
	if err != nil {
		return fmt.Errorf("enable %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Hold persistently disables the label in launchd's disable database; this
// survives reboot and apply until Release (ADR-0003).
func (r *Runtime) Hold(t runtime.Target) error {
	out, err := r.run("disable", Target(t.Label))
	if err != nil {
		return fmt.Errorf("disable %s: %s", t.Label, strings.TrimSpace(out))
	}
	return nil
}

// Start starts a loaded unit.
func (r *Runtime) Start(t runtime.Target) error { return r.kickstart(t.Label, false) }

// Restart restarts a loaded unit in place.
func (r *Runtime) Restart(t runtime.Target) error { return r.kickstart(t.Label, true) }

func (r *Runtime) kickstart(label string, kill bool) error {
	args := []string{"kickstart"}
	if kill {
		args = append(args, "-k")
	}
	args = append(args, Target(label))
	out, err := r.run(args...)
	if err != nil {
		return fmt.Errorf("kickstart %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Forget stops the label and clears its disable record, so a future Service
// reusing the label does not inherit an old local hold.
func (r *Runtime) Forget(ctx context.Context, label string) error {
	if err := r.unloadLabel(ctx, label); err != nil {
		return err
	}
	return r.enable(label)
}

// Info returns the normalized live state of the label.
func (r *Runtime) Info(t runtime.Target) (runtime.Info, error) {
	pi, err := r.printInfo(t.Label)
	if err != nil {
		return runtime.Info{}, err
	}
	return normalize(pi), nil
}

// normalize maps launchd's report onto keep's State. LastExit is carried
// through whenever launchd reports one, including for a running job, because
// callers distinguish "scheduled run that failed" from "not running".
func normalize(pi PrintInfo) runtime.Info {
	if !pi.Loaded {
		return runtime.Info{State: runtime.StateUnknown}
	}
	out := runtime.Info{State: runtime.StateStopped}
	if pi.HasLastExit {
		v := pi.LastExit
		out.LastExit = &v
	}
	switch {
	case pi.State == "running" || (pi.HasPID && pi.PID > 0):
		out.State = runtime.StateRunning
		out.PID = pi.PID
	case pi.HasLastExit && pi.LastExit != 0:
		out.State = runtime.StateFailed
	}
	return out
}

func (r *Runtime) printInfo(label string) (PrintInfo, error) {
	out, err := r.run("print", Target(label))
	if err != nil {
		if strings.Contains(out, "Could not find service") || strings.Contains(out, "could not find") {
			return ParsePrint(out, false), nil
		}
		return PrintInfo{}, fmt.Errorf("print %s: %s", label, strings.TrimSpace(out))
	}
	return ParsePrint(out, true), nil
}

// Held returns the label -> held map for the whole domain.
func (r *Runtime) Held() (map[string]bool, error) {
	out, err := r.run("print-disabled", Domain())
	if err != nil {
		return nil, fmt.Errorf("print-disabled: %s", strings.TrimSpace(out))
	}
	return ParseDisabled(out), nil
}

// Uptime returns a human-readable elapsed time for a running PID (via ps), or
// "" if it cannot be determined.
func (r *Runtime) Uptime(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
