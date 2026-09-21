package keep

import (
	"context"
	"fmt"

	"github.com/MaxAnderson95/keep/internal/config"
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
	target := m.target(s)
	if err := m.rt.Release(target); err != nil {
		return err
	}
	if err := m.rt.Load(context.Background(), target); err != nil {
		return err
	}
	if !s.IsScheduled() {
		info, err := m.rt.Info(target)
		if err == nil && !info.Running() {
			_ = m.rt.Start(target)
		}
	}
	return nil
}

// Down persistently holds a Service down. It stays down across reboot and
// apply until Up (ADR-0003). It returns once the Service has actually stopped,
// so ctx is what bounds a caller that cannot wait that long.
func (m *Manager) Down(ctx context.Context, s *config.Service) error {
	target := m.target(s)
	if err := m.rt.Hold(target); err != nil {
		return err
	}
	return m.rt.Unload(ctx, target)
}

// Bounce restarts a running Service in place.
func (m *Manager) Bounce(s *config.Service) error {
	return m.rt.Restart(m.target(s))
}
