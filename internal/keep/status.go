package keep

import (
	"fmt"
	"net"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
)

// Health is the rolled-up state keep reports for a Service.
type Health string

const (
	HealthRunning     Health = "running"      // resident, up
	HealthIdle        Health = "idle"         // scheduled, waiting to fire
	HealthHeld        Health = "held"         // down'd (intentional drift)
	HealthDeclaredOff Health = "declared-off" // enabled: false
	HealthStopped     Health = "stopped"      // resident, loaded but not running
	HealthNotLoaded   Health = "not-loaded"   // enabled but no live job (drift)
	HealthError       Health = "error"        // last exit non-zero
)

// ServiceStatus is the per-Service status snapshot (D10, D24).
type ServiceStatus struct {
	Name          string `json:"name"`
	Label         string `json:"label"`
	Type          string `json:"type"`
	Enabled       bool   `json:"enabled"`
	Health        Health `json:"health"`
	Loaded        bool   `json:"loaded"`
	PID           int    `json:"pid,omitempty"`
	Uptime        string `json:"uptime,omitempty"`
	LastExit      *int   `json:"last_exit,omitempty"`
	Held          bool   `json:"held"`
	DeclaredOff   bool   `json:"declared_off"`
	Drift         bool   `json:"drift"`
	Port          int    `json:"port,omitempty"`
	PortListening *bool  `json:"port_listening,omitempty"`
}

// Status returns status for the named Services (all if names is empty).
func (m *Manager) Status(names []string) ([]ServiceStatus, error) {
	targets, err := m.Targets(names)
	if err != nil {
		return nil, err
	}
	disabled, err := m.ctl.DisabledSet()
	if err != nil {
		return nil, err
	}
	out := make([]ServiceStatus, 0, len(targets))
	for _, s := range targets {
		st, err := m.statusOf(s, disabled)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func (m *Manager) statusOf(s *config.Service, disabled map[string]bool) (ServiceStatus, error) {
	label := s.EffectiveLabel()
	info, err := m.ctl.Info(label)
	if err != nil {
		return ServiceStatus{}, err
	}
	st := ServiceStatus{
		Name:    s.Name,
		Label:   label,
		Type:    s.Type,
		Enabled: s.IsEnabled(),
		Loaded:  info.Loaded,
		Port:    s.Port,
	}
	if info.HasPID {
		st.PID = info.PID
		st.Uptime = m.ctl.Uptime(info.PID)
	}
	if info.HasLastExit {
		v := info.LastExit
		st.LastExit = &v
	}

	switch {
	case !s.IsEnabled():
		st.DeclaredOff = true
		st.Health = HealthDeclaredOff
		// live-enabled while declared off is drift
		if info.Loaded && !disabled[label] {
			st.Drift = true
		}
	case disabled[label]:
		st.Held = true
		st.Drift = true
		st.Health = HealthHeld
	case s.IsScheduled():
		st.Health = HealthIdle
		if info.HasLastExit && info.LastExit != 0 {
			st.Health = HealthError
		}
		if !info.Loaded {
			st.Health = HealthNotLoaded
			st.Drift = true
		}
	default: // resident, enabled, not held
		switch {
		case !info.Loaded:
			st.Health = HealthNotLoaded
			st.Drift = true
		case isRunning(info):
			st.Health = HealthRunning
		case info.HasLastExit && info.LastExit != 0:
			st.Health = HealthError
			st.Drift = true
		default:
			st.Health = HealthStopped
			st.Drift = true
		}
	}

	// Optional port-listening liveness check (D10, issue #9).
	if s.Port > 0 && isRunning(info) {
		listening := portListening(s.Port)
		st.PortListening = &listening
	}
	return st, nil
}

func portListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
