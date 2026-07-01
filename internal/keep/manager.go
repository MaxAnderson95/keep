// Package keep is the orchestration layer: it turns the declarative Config into
// generated artifacts and reconciles them against live launchd state. The CLI
// and TUI are thin shells over this package.
package keep

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/launchd"
)

// Manager owns a loaded Config plus the resolved keep binary path that gets
// pinned into every generated artifact (D15, ADR-0002). It drives launchd
// through the Controller seam and writes generated artifacts under agentsDir,
// both injectable so the orchestration logic is testable off a live machine.
type Manager struct {
	Cfg      *config.Config
	KeepPath string // pinned absolute path to the keep binary
	Version  string

	ctl       launchd.Controller // the launchd control seam
	agentsDir string             // where generated artifacts are written
}

// NewManager builds a Manager wired to the production launchd Controller and
// the user LaunchAgents directory, resolving the keep binary's own path.
func NewManager(cfg *config.Config, version string) (*Manager, error) {
	kp, err := resolveSelf()
	if err != nil {
		return nil, err
	}
	return &Manager{
		Cfg:       cfg,
		KeepPath:  kp,
		Version:   version,
		ctl:       launchd.NewCLI(),
		agentsDir: config.LaunchAgentsDir(),
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

// LaunchAgentsDir is where generated artifacts are written.
func (m *Manager) LaunchAgentsDir() string {
	if m.agentsDir != "" {
		return m.agentsDir
	}
	return config.LaunchAgentsDir()
}

// PlistPath is the on-disk path of a Service's generated artifact.
func (m *Manager) PlistPath(s *config.Service) string {
	return filepath.Join(m.LaunchAgentsDir(), s.EffectiveLabel()+".plist")
}

// BuildJob composes the launchd Job for a Service: the pinned keep path plus
// `fork <name>` (ADR-0002), the marker, log paths, and resident/scheduled
// specifics.
func (m *Manager) BuildJob(s *config.Service) (launchd.Job, error) {
	args := []string{m.KeepPath}
	// Stamp a non-default config path so fork loads the same Config.
	if m.Cfg.Path != "" && m.Cfg.Path != config.DefaultConfigPath() {
		args = append(args, "--config", m.Cfg.Path)
	}
	args = append(args, "fork", s.Name)

	job := launchd.Job{
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
				return launchd.Job{}, err
			}
			job.StartInterval = secs
		}
		for _, ci := range sched.Calendar {
			job.StartCalendar = append(job.StartCalendar, launchd.CalendarInterval{
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

// PlistBytes renders a Service's generated artifact.
func (m *Manager) PlistBytes(s *config.Service) ([]byte, error) {
	job, err := m.BuildJob(s)
	if err != nil {
		return nil, err
	}
	return launchd.Render(job), nil
}

// ensureLogDir creates the log directory for a Service if needed.
func (m *Manager) ensureLogDir(s *config.Service) error {
	dir := m.Cfg.ResolveLogDir(s)
	return os.MkdirAll(dir, 0o755)
}
