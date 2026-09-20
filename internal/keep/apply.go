package keep

import (
	"bytes"
	"context"
	"fmt"
	"os"

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

// Apply reconciles live runtime state to the Config (ADR-0001). It creates,
// updates, and prunes managed Services, respects holds, and never touches
// unmanaged units.
//
// Apply is CLI-only — the web API exposes plan and diff, never apply — so it
// has no caller to cancel it: the Unload calls below pass context.Background()
// and lean on Unload's own settle ceiling.
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
		target := m.target(s)

		if err := m.ensureLogDir(s); err != nil {
			return res, fmt.Errorf("service %q: %w", s.Name, err)
		}
		desired, err := m.Artifacts(s)
		if err != nil {
			return res, err
		}
		for _, a := range desired {
			if err := writeIfChanged(a.Path, a.Data); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
		}

		// Declared off (enabled: false): generate but keep disabled. Not drift.
		if !s.IsEnabled() {
			if err := m.rt.Hold(target); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
			if err := m.rt.Unload(context.Background(), target); err != nil {
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
		default: // noop — ensure the runtime actually knows about it
			info, err := m.rt.Info(target)
			if err != nil {
				return res, err
			}
			if !info.Known() {
				if err := m.loadService(s); err != nil {
					return res, err
				}
			}
			res.Unchanged = append(res.Unchanged, s.Name)
		}
	}

	removed, err := m.prune(plan.Removes)
	res.Removed = removed
	if err != nil {
		return res, err
	}

	// Drop cached versions that can no longer be displayed — removed Services
	// and edited version_commands (D26). Best-effort: stale display metadata
	// must not fail a reconcile.
	m.pruneVersionEntries()
	return res, nil
}

// prune removes orphaned Services: the artifact files go first, so a runtime
// that re-reads disk while forgetting the label does not find them still
// there.
func (m *Manager) prune(removes []ServicePlan) ([]string, error) {
	if len(removes) == 0 {
		return nil, nil
	}
	managed, err := m.ScanManaged()
	if err != nil {
		return nil, err
	}
	_, byLabel := m.orphanLabels(managed)
	var removed []string
	for _, rm := range removes {
		for _, a := range byLabel[rm.Label] {
			if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
			}
		}
		if err := m.rt.Forget(context.Background(), rm.Label); err != nil {
			return removed, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
		}
		removed = append(removed, rm.Name)
	}
	return removed, nil
}

// loadService releases any hold and has the runtime pick the Service up from
// the artifacts on disk.
func (m *Manager) loadService(s *config.Service) error {
	target := m.target(s)
	if err := m.rt.Release(target); err != nil {
		return err
	}
	// Resident services start on load; scheduled services wait for their next
	// fire. Nothing to Start here.
	return m.rt.Load(context.Background(), target)
}

// reloadService stops the Service and loads the regenerated artifacts.
func (m *Manager) reloadService(s *config.Service) error {
	if err := m.rt.Unload(context.Background(), m.target(s)); err != nil {
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
