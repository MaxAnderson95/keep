package keep

import (
	"bytes"
	"path/filepath"
	"strings"
)

// ChangeKind is what apply would do to a Service's generated artifact.
type ChangeKind string

const (
	ChangeAdd    ChangeKind = "add"
	ChangeUpdate ChangeKind = "update"
	ChangeNoop   ChangeKind = "noop"
	ChangeRemove ChangeKind = "remove"
)

// ServicePlan is the planned action and drift state for one Service.
type ServicePlan struct {
	Name          string     `json:"name"`
	Label         string     `json:"label"`
	Kind          ChangeKind `json:"kind"`
	Held          bool       `json:"held"`           // live-disabled + config-enabled (intentional drift)
	DeclaredOff   bool       `json:"declared_off"`   // config enabled:false (not drift)
	DisabledDrift bool       `json:"disabled_drift"` // live-enabled + config-disabled (drift)
	Reason        string     `json:"reason,omitempty"`
}

// Plan is what diff reports and apply executes.
type Plan struct {
	Services []ServicePlan `json:"services"`
	Removes  []ServicePlan `json:"removes"`
}

// HasChanges reports whether the plan would mutate anything.
func (p Plan) HasChanges() bool {
	for _, s := range p.Services {
		if s.Kind != ChangeNoop {
			return true
		}
	}
	return len(p.Removes) > 0
}

// HasDrift reports whether the plan surfaces any drift.
func (p Plan) HasDrift() bool {
	for _, s := range p.Services {
		if s.Held || s.DisabledDrift || s.Kind == ChangeUpdate || s.Kind == ChangeAdd {
			return true
		}
	}
	return len(p.Removes) > 0
}

// ComputePlan diffs the Config against live runtime state without mutating it.
// A Service's artifacts roll up into one entry: whatever the runtime renders
// for it, keep plans and applies it as a single Service.
func (m *Manager) ComputePlan() (Plan, error) {
	managed, err := m.ScanManaged()
	if err != nil {
		return Plan{}, err
	}
	byPath := map[string]ManagedArtifact{}
	for _, a := range managed {
		byPath[a.Path] = a
	}
	held, err := m.rt.Held()
	if err != nil {
		return Plan{}, err
	}

	var plan Plan
	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		label := s.EffectiveLabel()
		desired, err := m.Artifacts(s)
		if err != nil {
			return Plan{}, err
		}
		sp := ServicePlan{Name: s.Name, Label: label}

		var missing, differing []string
		for _, a := range desired {
			switch existing, ok := byPath[a.Path]; {
			case !ok:
				missing = append(missing, filepath.Base(a.Path))
			case !bytes.Equal(existing.Data, a.Data):
				differing = append(differing, filepath.Base(a.Path))
			}
		}
		switch {
		case len(missing) > 0:
			sp.Kind = ChangeAdd
			sp.Reason = "no generated artifact yet: " + strings.Join(missing, ", ")
		case len(differing) > 0:
			sp.Kind = ChangeUpdate
			sp.Reason = "generated artifact differs from Config (changed or hand-edited): " +
				strings.Join(differing, ", ")
		default:
			sp.Kind = ChangeNoop
		}

		isHeld := held[label]
		switch {
		case !s.IsEnabled():
			sp.DeclaredOff = true
			if len(missing) == 0 && !isHeld {
				sp.DisabledDrift = true
				sp.Reason = appendReason(sp.Reason, "declared off but currently enabled")
			}
		case isHeld:
			sp.Held = true
			sp.Reason = appendReason(sp.Reason, "held down (declared enabled, currently disabled)")
		}

		plan.Services = append(plan.Services, sp)
	}

	labels, byLabel := m.orphanLabels(managed)
	for _, label := range labels {
		plan.Removes = append(plan.Removes, ServicePlan{
			Name:   byLabel[label][0].Service,
			Label:  label,
			Kind:   ChangeRemove,
			Reason: "managed artifact no longer in Config",
		})
	}
	return plan, nil
}

func appendReason(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}
