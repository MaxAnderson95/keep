package serve

import (
	"errors"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

// orchestrator is the slice of keep.Manager the web layer needs. It exists so
// handler tests can run against a fake without touching launchd.
type orchestrator interface {
	Targets(names []string) ([]*config.Service, error)
	Status(names []string) ([]keep.ServiceStatus, error)
	Up(s *config.Service) error
	Down(s *config.Service) error
	Bounce(s *config.Service) error
	Show(name string) (keep.Resolved, error)
	ComputePlan() (keep.Plan, error)
	Doctor() ([]keep.Finding, error)
	LogTargets(names []string) ([]keep.LogTarget, error)
}

// configError marks a Config that failed to load or validate, so the API can
// answer 503 config_invalid while the server itself stays up (W9).
type configError struct{ err error }

func (e configError) Error() string { return e.err.Error() }
func (e configError) Unwrap() error { return e.err }

func isConfigError(err error) bool {
	var ce configError
	return errors.As(err, &ce)
}

// configPath is the Config this server reads on every request.
func (s *Server) configPath() string {
	if s.opts.ConfigPath != "" {
		return s.opts.ConfigPath
	}
	return config.DefaultConfigPath()
}

// defaultOrchestrator loads the Config fresh from disk and builds a real
// Manager — every request sees exactly what a CLI invocation would (W9).
func (s *Server) defaultOrchestrator() (orchestrator, error) {
	cfg, err := config.Load(s.configPath())
	if err != nil {
		return nil, configError{err}
	}
	return keep.NewManager(cfg, s.opts.Version)
}
