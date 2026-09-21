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
	// The Manager owns every artifact write, including making somewhere to
	// write them. ~/Library/LaunchAgents always exists; a machine that has
	// never had a user unit has no ~/.config/systemd/user.
	if err := os.MkdirAll(m.ArtifactDir(), 0o755); err != nil {
		return res, fmt.Errorf("creating artifact directory: %w", err)
	}
	managed, err := m.ScanManaged()
	if err != nil {
		return res, err
	}
	rendered, claimed, err := m.allArtifacts()
	if err != nil {
		return res, err
	}

	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		sp := planByName[s.Name]
		target := m.target(s)

		if err := m.ensureLogDir(s); err != nil {
			return res, fmt.Errorf("service %q: %w", s.Name, err)
		}
		desired := rendered[s.Name]
		for _, a := range desired {
			if err := writeIfChanged(a.Path, a.Data); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
		}

		// Retire artifacts this Service used to have. A scheduled Service that
		// became resident leaves an enabled timer behind, and that timer keeps
		// starting the Service — including after a `keep down`, which now
		// addresses only the resident unit. Retiring comes before the
		// held and declared-off branches below, because the whole point is
		// that a Service which is supposed to be stopped stays stopped.
		stale := staleArtifacts(managed, s.Name, claimed)
		if len(stale) > 0 {
			if err := m.retire(stale); err != nil {
				return res, fmt.Errorf("service %q: %w", s.Name, err)
			}
			// Forget clears the runtime's persistent records for a label, and
			// a stale artifact can share the Service's current label, so a
			// hold may have gone with it. Re-assert it rather than depend on
			// which way a given runtime's Forget happens to fall.
			if sp.Held {
				if err := m.rt.Hold(target); err != nil {
					return res, fmt.Errorf("service %q: %w", s.Name, err)
				}
			}
			// Whatever the plan said, the Service's shape changed on disk.
			sp.Kind = ChangeUpdate
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

	removed, err := m.prune(plan.Removes, managed, claimed)
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
// there. A failed Forget puts them back, because those files are the only
// record that lets the next apply rediscover the orphan — without the restore,
// a runtime error here would leave a Service running that keep could never see
// again.
// prune removes the artifacts of Services that left the Config. An artifact a
// declared Service has since claimed is left alone: the file was taken over,
// not abandoned, and it has already been rewritten for its new owner.
func (m *Manager) prune(removes []ServicePlan, managed []ManagedArtifact, claimed map[string]bool) ([]string, error) {
	if len(removes) == 0 {
		return nil, nil
	}
	_, orphaned := m.orphanLabels(managed)
	var removed []string
	for _, rm := range removes {
		var abandoned []ManagedArtifact
		for _, a := range orphaned[rm.Label] {
			if !claimed[a.Path] {
				abandoned = append(abandoned, a)
			}
		}
		if err := m.forgetArtifacts(rm.Label, abandoned); err != nil {
			return removed, fmt.Errorf("removing orphan %q: %w", rm.Label, err)
		}
		removed = append(removed, rm.Name)
	}
	return removed, nil
}

// retire removes artifacts a Service no longer renders, and stops and clears
// whatever the runtime was still doing with them.
func (m *Manager) retire(stale []ManagedArtifact) error {
	labels, grouped := byLabel(stale)
	for _, label := range labels {
		if err := m.forgetArtifacts(label, grouped[label]); err != nil {
			return fmt.Errorf("retiring %q: %w", label, err)
		}
	}
	return nil
}

// forgetArtifacts deletes the given artifacts and then has the runtime forget
// exactly those, never the whole label: a sibling artifact under the same
// label may still be in service, sometimes for a different Service. The files
// go first, so a runtime that re-reads disk while forgetting does not find
// them still there; a failed Forget puts them back, because they are the only
// record that would let the next apply retry.
func (m *Manager) forgetArtifacts(label string, arts []ManagedArtifact) error {
	if len(arts) == 0 {
		return nil
	}
	paths := make([]string, 0, len(arts))
	for _, a := range arts {
		if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		paths = append(paths, a.Path)
	}
	if err := m.rt.Forget(context.Background(), label, paths); err != nil {
		restore(arts)
		return err
	}
	return nil
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

// restore puts scanned artifacts back after a failed prune. Best-effort: the
// error that triggered the restore is the one worth reporting, and a write
// that fails here leaves exactly the state the caller is already being told
// about.
func restore(artifacts []ManagedArtifact) {
	for _, a := range artifacts {
		_ = os.WriteFile(a.Path, a.Data, 0o644)
	}
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
