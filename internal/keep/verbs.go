package keep

import (
	"fmt"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/launchd"
)

// Targets resolves a list of Service names to Services. An empty list means
// all declared Services.
func (m *Manager) Targets(names []string) ([]*config.Service, error) {
	if len(names) == 0 {
		out := make([]*config.Service, 0, len(m.Cfg.Services))
		for i := range m.Cfg.Services {
			out = append(out, &m.Cfg.Services[i])
		}
		return out, nil
	}
	var out []*config.Service
	for _, n := range names {
		s, ok := m.Cfg.Service(n)
		if !ok {
			return nil, fmt.Errorf("unknown service %q", n)
		}
		out = append(out, s)
	}
	return out, nil
}

// Up enables and starts a Service (ADR-0003). For resident Services it ensures
// the process is actually running; scheduled Services are left to fire on their
// schedule rather than being force-run.
func (m *Manager) Up(s *config.Service) error {
	label := s.EffectiveLabel()
	if err := m.ctl.Enable(label); err != nil {
		return err
	}
	if err := m.ctl.Bootstrap(m.PlistPath(s)); err != nil {
		return err
	}
	if !s.IsScheduled() {
		info, err := m.ctl.Info(label)
		if err == nil && !isRunning(info) {
			_ = m.ctl.Kickstart(label, false)
		}
	}
	return nil
}

// Down persistently holds a Service down: disable + bootout. It stays down
// across reboot and apply until Up (ADR-0003).
func (m *Manager) Down(s *config.Service) error {
	label := s.EffectiveLabel()
	if err := m.ctl.Disable(label); err != nil {
		return err
	}
	return m.ctl.Bootout(label)
}

// Bounce restarts a running Service in place (kickstart -k).
func (m *Manager) Bounce(s *config.Service) error {
	return m.ctl.Kickstart(s.EffectiveLabel(), true)
}

func isRunning(info launchd.PrintInfo) bool {
	return info.Loaded && (info.State == "running" || info.HasPID && info.PID > 0)
}
