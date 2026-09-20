// Package keep is the orchestration layer: it turns the declarative Config into
// generated artifacts and reconciles them against the live state of the OS
// service runtime. The CLI and TUI are thin shells over this package.
package keep

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/runtime"
)

// Manager owns a loaded Config plus the resolved keep binary path that gets
// pinned into every generated artifact (D15, ADR-0002). It drives the OS
// service runtime through the Runtime seam, and it owns every write to the
// artifacts that runtime renders. The seam is injectable, so the orchestration
// logic is testable off a live machine (ADR-0008).
type Manager struct {
	Cfg      *config.Config
	KeepPath string // pinned absolute path to the keep binary
	Version  string

	rt       runtime.Runtime // the service runtime seam
	stateDir string          // machine-local state (update locks)
}

// NewManager builds a Manager wired to this machine's service runtime adapter,
// resolving the keep binary's own path.
func NewManager(cfg *config.Config, version string) (*Manager, error) {
	kp, err := resolveSelf()
	if err != nil {
		return nil, err
	}
	rt, err := detectRuntime()
	if err != nil {
		return nil, err
	}
	return &Manager{
		Cfg:      cfg,
		KeepPath: kp,
		Version:  version,
		rt:       rt,
	}, nil
}

func resolveSelf() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving keep path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Clean(exe), nil
}

// ArtifactDir is where generated artifacts are written: the directory the
// runtime loads them from.
func (m *Manager) ArtifactDir() string { return m.rt.ArtifactDir() }

// StateDir is where keep keeps machine-local state (e.g. update locks).
func (m *Manager) StateDir() string {
	if m.stateDir != "" {
		return m.stateDir
	}
	return config.StateDir()
}

// target identifies a Service's unit(s) to the runtime. Scheduled travels with
// it so an adapter can route control to a different unit than a resident
// Service's without probing disk.
func (m *Manager) target(s *config.Service) runtime.Target {
	return runtime.Target{Label: s.EffectiveLabel(), Scheduled: s.IsScheduled()}
}

// BuildJob composes the OS-neutral Job for a Service: the pinned keep path plus
// `fork <name>` (ADR-0002), the marker, log paths, and resident/scheduled
// specifics.
func (m *Manager) BuildJob(s *config.Service) (runtime.Job, error) {
	args := []string{m.KeepPath}
	// Stamp a non-default config path so fork loads the same Config.
	if m.Cfg.Path != "" && m.Cfg.Path != config.DefaultConfigPath() {
		args = append(args, "--config", m.Cfg.Path)
	}
	args = append(args, "fork", s.Name)

	job := runtime.Job{
		Label:             s.EffectiveLabel(),
		ProgramArguments:  args,
		StandardOutPath:   m.Cfg.StdoutPath(s),
		StandardErrorPath: m.Cfg.StderrPath(s),
		Service:           s.Name,
		KeepVersion:       m.Version,
		KeepPath:          m.KeepPath,
	}

	if s.IsScheduled() {
		sched := s.Schedule
		if sched.Interval != "" {
			secs, err := config.ParseInterval(sched.Interval)
			if err != nil {
				return runtime.Job{}, err
			}
			job.StartInterval = secs
		}
		for _, ci := range sched.Calendar {
			job.StartCalendar = append(job.StartCalendar, runtime.CalendarInterval{
				Minute:  ci.Minute,
				Hour:    ci.Hour,
				Day:     ci.Day,
				Weekday: ci.Weekday,
				Month:   ci.Month,
			})
		}
	} else {
		job.RunAtLoad = true
		job.KeepAlive = true
	}
	return job, nil
}

// Artifacts renders every file the runtime needs for a Service. The Manager,
// never the adapter, decides whether and where they get written.
func (m *Manager) Artifacts(s *config.Service) ([]runtime.Artifact, error) {
	job, err := m.BuildJob(s)
	if err != nil {
		return nil, err
	}
	return m.rt.Render(job), nil
}

// ensureLogDir creates the log directory for a Service if needed.
func (m *Manager) ensureLogDir(s *config.Service) error {
	dir := m.Cfg.ResolveLogDir(s)
	return os.MkdirAll(dir, 0o755)
}
