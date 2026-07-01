package keep

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/config"
)

// ApplyResult summarizes what apply did (D24, apply --json).
type ApplyResult struct {
	Added       []string `json:"added"`
	Updated     []string `json:"updated"`
	Removed     []string `json:"removed"`
	Unchanged   []string `json:"unchanged"`
	Held        []string `json:"held"`         // respected holds, left down
	DeclaredOff []string `json:"declared_off"` // generated but kept disabled
}

// Apply reconciles live launchd state to the Config (ADR-0001). It creates,
// updates, and prunes managed Services, respects holds, and never touches
// unmanaged jobs.
func (m *Manager) Apply() (ApplyResult, error) {
	plan, err := m.ComputePlan()
	if err != nil {
		return ApplyResult{}, err
	}
	planByName := map[string]ServicePlan{}
	for _, sp := range plan.Services {
		planByName[sp.Name] = sp
	}

	var res ApplyResult
	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		sp := planByName[s.Name]
		label := s.EffectiveLabel()

		if err := m.ensureLogDir(s); err != nil {
			return res, fmt.Errorf("service %q: %w", s.Name, err)
		}
		desired, err := m.PlistBytes(s)
		if err != nil {
			return res, err
		}
		if err := writeIfChanged(m.PlistPath(s), desired); err != nil {
			return res, fmt.Errorf("service %q: %w", s.Name, err)
		}

		// Declared off (enabled: false): generate but keep disabled. Not drift.
		if !s.IsEnabled() {
			if err := m.ctl.Disable(label); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
			if err := m.ctl.Bootout(label); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
			res.DeclaredOff = append(res.DeclaredOff, s.Name)
			continue
		}

		// Respect a hold: declared enabled but currently held down — do not
		// resurrect it (ADR-0003).
		if sp.Held {
			res.Held = append(res.Held, s.Name)
			continue
		}

		switch sp.Kind {
		case ChangeAdd:
			if err := m.loadService(s); err != nil {
				return res, err
			}
			res.Added = append(res.Added, s.Name)
		case ChangeUpdate:
			if err := m.reloadService(s); err != nil {
				return res, err
			}
			res.Updated = append(res.Updated, s.Name)
		default: // noop — ensure it is actually loaded
			info, err := m.ctl.Info(label)
			if err != nil {
				return res, err
			}
			if !info.Loaded {
				if err := m.loadService(s); err != nil {
					return res, err
				}
			}
			res.Unchanged = append(res.Unchanged, s.Name)
		}
	}

	// Prune orphans — only ever managed artifacts.
	for _, rm := range plan.Removes {
		if err := m.ctl.Bootout(rm.Label); err != nil {
			return res, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
		}
		// Clear any stale disable record so a future Service with the same label
		// does not inherit an old local hold.
		if err := m.ctl.Enable(rm.Label); err != nil {
			return res, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
		}
		path := filepath.Join(m.LaunchAgentsDir(), rm.Label+".plist")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return res, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
		}
		res.Removed = append(res.Removed, rm.Name)
	}
	return res, nil
}

// loadService enables and bootstraps a Service so launchd tracks it.
func (m *Manager) loadService(s *config.Service) error {
	label := s.EffectiveLabel()
	if err := m.ctl.Enable(label); err != nil {
		return err
	}
	if err := m.ctl.Bootstrap(m.PlistPath(s)); err != nil {
		return err
	}
	// Resident services carry RunAtLoad and start on bootstrap; scheduled
	// services wait for their next fire. Nothing to kickstart here.
	return nil
}

// reloadService boots out the old job and bootstraps the regenerated plist.
func (m *Manager) reloadService(s *config.Service) error {
	label := s.EffectiveLabel()
	if err := m.ctl.Bootout(label); err != nil {
		return err
	}
	return m.loadService(s)
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
