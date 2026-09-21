package systemd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// MinVersion is the oldest systemd keep supports. StandardOutput=append:
// landed in 240, and keep writes Service logs to files rather than journald so
// that `keep logs`, rotation, and the web UI work the same on both OSes.
const MinVersion = 240

// Runtime is the systemd adapter. It drives the per-user service manager
// (`systemctl --user`), so nothing here needs root, exactly like the launchd
// adapter's GUI domain.
type Runtime struct {
	// run executes systemctl; an internal seam so the adapter's own wiring is
	// not hard-bound to exec. Not exposed — keep's tests use an in-memory
	// adapter at the runtime.Runtime interface instead.
	run func(ctx context.Context, args ...string) (string, error)
	// loginctl answers the lingering question, which systemctl cannot.
	loginctl    func(ctx context.Context, args ...string) (string, error)
	artifactDir string
}

// New returns the systemctl-backed Runtime.
func New() *Runtime {
	return &Runtime{run: execSystemctl, loginctl: execLoginctl, artifactDir: UserUnitDir()}
}

func execSystemctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// UserUnitDir is where the systemd user manager reads unit files written by
// the user.
func UserUnitDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "systemd", "user")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "systemd", "user")
	}
	return filepath.Join(home, ".config", "systemd", "user")
}

// ArtifactDir is the user unit directory.
func (r *Runtime) ArtifactDir() string { return r.artifactDir }

// ReadMarkers parses keep's marker section out of a unit file, deriving the
// label from the filename stem. Anything that is not a unit keep emits, or
// carries no marker, is not keep's.
func (r *Runtime) ReadMarkers(path string, data []byte) runtime.MarkerInfo {
	ext := filepath.Ext(path)
	if ext != serviceExt && ext != timerExt {
		return runtime.MarkerInfo{}
	}
	managed, service, keepPath := readMarkers(data)
	if !managed {
		return runtime.MarkerInfo{}
	}
	return runtime.MarkerInfo{
		Managed:  true,
		Label:    strings.TrimSuffix(filepath.Base(path), ext),
		Service:  service,
		KeepPath: keepPath,
	}
}

// unit is the unit systemd control verbs address for a Target: the timer for a
// scheduled Service, the service itself otherwise.
func unit(t runtime.Target) string {
	if t.Scheduled {
		return t.Label + timerExt
	}
	return t.Label + serviceExt
}

// notLoaded reports whether systemctl's complaint just means the unit is not
// there, which every idempotent verb treats as success.
func notLoaded(out string) bool {
	lowered := strings.ToLower(out)
	for _, s := range []string{"not loaded", "could not be found", "no such file or directory", "does not exist"} {
		if strings.Contains(lowered, s) {
			return true
		}
	}
	return false
}

// Load makes the user manager pick up the artifacts the Manager just wrote and
// starts the unit. daemon-reload comes first: systemd caches unit files, so
// starting without it would run the previous generation.
//
// ctx is unused: `start` on a Type=exec unit returns once the process is up,
// and neither call waits on anything keep needs to abandon.
func (r *Runtime) Load(ctx context.Context, t runtime.Target) error {
	if err := r.daemonReload(ctx); err != nil {
		return err
	}
	return r.startUnit(ctx, "start", unit(t))
}

func (r *Runtime) daemonReload(ctx context.Context) error {
	if out, err := r.run(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %s", strings.TrimSpace(out))
	}
	return nil
}

func (r *Runtime) startUnit(ctx context.Context, verb, name string) error {
	if out, err := r.run(ctx, verb, name); err != nil {
		return fmt.Errorf("%s %s: %s", verb, name, strings.TrimSpace(out))
	}
	return nil
}

// Unload stops the unit and, for a scheduled Service, the run it may have in
// flight. `systemctl stop` does not return until the unit is down, so unlike
// launchd's bootout there is no teardown to settle afterwards.
func (r *Runtime) Unload(ctx context.Context, t runtime.Target) error {
	if err := r.stop(ctx, unit(t)); err != nil {
		return err
	}
	if t.Scheduled {
		// Stopping the timer only disarms it; a run already under way keeps
		// going, and Down promises the Service is actually stopped.
		return r.stop(ctx, t.Label+serviceExt)
	}
	return nil
}

func (r *Runtime) stop(ctx context.Context, name string) error {
	out, err := r.run(ctx, "stop", name)
	if err != nil && !notLoaded(out) {
		return fmt.Errorf("stop %s: %s", name, strings.TrimSpace(out))
	}
	return nil
}

// Hold persistently disables the unit, so it does not come back at login or on
// the next apply (ADR-0003).
func (r *Runtime) Hold(t runtime.Target) error {
	if out, err := r.run(context.Background(), "disable", unit(t)); err != nil && !notLoaded(out) {
		return fmt.Errorf("disable %s: %s", unit(t), strings.TrimSpace(out))
	}
	return nil
}

// Release clears a Hold.
func (r *Runtime) Release(t runtime.Target) error {
	if out, err := r.run(context.Background(), "enable", unit(t)); err != nil {
		return fmt.Errorf("enable %s: %s", unit(t), strings.TrimSpace(out))
	}
	return nil
}

// Start starts a loaded unit.
func (r *Runtime) Start(t runtime.Target) error {
	return r.startUnit(context.Background(), "start", unit(t))
}

// Restart restarts a loaded unit in place.
func (r *Runtime) Restart(t runtime.Target) error {
	return r.startUnit(context.Background(), "restart", unit(t))
}

// Forget stops and disables the units behind the given artifacts, then reloads
// so the manager drops the files the Manager has already deleted.
//
// Only the named artifacts' units are touched. A Service that stops being
// scheduled gives up its .timer while its .service lives on — sometimes under
// another Service — and stopping the whole label would take that one down too.
// With no artifacts named, a pruned Service's units cannot be enumerated, so
// every form the label could have taken is forgotten.
func (r *Runtime) Forget(ctx context.Context, label string, paths []string) error {
	for _, name := range unitNames(label, paths) {
		if err := r.stop(ctx, name); err != nil {
			return err
		}
		if out, err := r.run(ctx, "disable", name); err != nil && !notLoaded(out) {
			return fmt.Errorf("disable %s: %s", name, strings.TrimSpace(out))
		}
		// systemd remembers a failed unit after its file is gone, so a pruned
		// Service would keep showing up in `systemctl --user --failed` until
		// the next reboot. Best-effort: there is usually nothing to reset, and
		// a Service that is already gone must not fail the prune.
		_, _ = r.run(ctx, "reset-failed", name)
	}
	return r.daemonReload(ctx)
}

// unitNames maps the artifacts keep is removing to the units that run them.
// The adapter named those files, so their basenames are the unit names.
func unitNames(label string, paths []string) []string {
	if len(paths) == 0 {
		return []string{label + timerExt, label + serviceExt}
	}
	var names []string
	for _, p := range paths {
		if base := filepath.Base(p); filepath.Ext(base) == timerExt || filepath.Ext(base) == serviceExt {
			names = append(names, base)
		}
	}
	return names
}

// showProperties is what Info asks systemctl for.
var showProperties = "LoadState,ActiveState,SubState,MainPID,ExecMainStatus,Result"

// Info returns the normalized live state of a Target. For a scheduled Service
// the timer carries the state (armed and waiting) while the service carries
// the result of the last run, so both are read.
func (r *Runtime) Info(t runtime.Target) (runtime.Info, error) {
	props, err := r.show(unit(t))
	if err != nil {
		return runtime.Info{}, err
	}
	info := normalize(props)
	if t.Scheduled && info.State != runtime.StateUnknown {
		runProps, err := r.show(t.Label + serviceExt)
		if err != nil {
			return runtime.Info{}, err
		}
		if runProps.HasExitStatus {
			v := runProps.ExecMainStatus
			info.LastExit = &v
		}
		// A run in flight is the Service's live process.
		if runProps.MainPID > 0 {
			info.PID = runProps.MainPID
		}
	}
	return info, nil
}

func (r *Runtime) show(name string) (showProps, error) {
	out, err := r.run(context.Background(), "show", name, "-p", showProperties)
	if err != nil {
		if notLoaded(out) {
			return showProps{LoadState: "not-found"}, nil
		}
		return showProps{}, fmt.Errorf("show %s: %s", name, strings.TrimSpace(out))
	}
	return parseShow(out), nil
}

// normalize maps systemd's vocabulary onto keep's State.
func normalize(p showProps) runtime.Info {
	if p.LoadState == "not-found" || p.LoadState == "" {
		return runtime.Info{State: runtime.StateUnknown}
	}
	out := runtime.Info{State: runtime.StateStopped}
	if p.HasExitStatus {
		v := p.ExecMainStatus
		out.LastExit = &v
	}
	switch {
	case p.MainPID > 0:
		out.State = runtime.StateRunning
		out.PID = p.MainPID
	case p.Result != "" && p.Result != "success":
		out.State = runtime.StateFailed
	}
	return out
}

// Held returns stem -> held for every unit file the manager knows.
func (r *Runtime) Held() (map[string]bool, error) {
	out, err := r.run(context.Background(), "list-unit-files", "--no-legend", "--no-pager", "--type=service,timer")
	if err != nil {
		return nil, fmt.Errorf("list-unit-files: %s", strings.TrimSpace(out))
	}
	return parseUnitFiles(out), nil
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
