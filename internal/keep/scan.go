package keep

import (
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// ManagedArtifact is a generated file on disk that carries keep's marker.
type ManagedArtifact struct {
	Path     string
	Label    string
	Service  string
	KeepPath string
	Data     []byte
}

// ScanManaged finds every keep-managed artifact in the runtime's artifact
// directory. It never reports unmanaged units — the marker is the boundary
// (D2, D19) — and it asks the runtime what its own output looks like rather
// than assuming a file type or a file per Service.
func (m *Manager) ScanManaged() ([]ManagedArtifact, error) {
	dir := m.ArtifactDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []ManagedArtifact
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		// Regular files only, resolved through symlinks. Without the old
		// extension filter there is nothing else standing between the scan and
		// whatever shares the directory, and reading a FIFO with no writer
		// would block apply, diff, and doctor forever.
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		marker := m.rt.ReadMarkers(path, data)
		if !marker.Managed {
			continue
		}
		found = append(found, ManagedArtifact{
			Path:     path,
			Label:    marker.Label,
			Service:  marker.Service,
			KeepPath: marker.KeepPath,
			Data:     data,
		})
	}
	return found, nil
}

// staleArtifacts returns managed artifacts that belong to a Service but that
// the Service's current shape no longer renders: the leftover `.timer` of a
// scheduled Service that became resident, or an artifact under a label the
// Config has since renamed. They are not orphans — the Service is still
// declared — and nothing else would ever remove them.
func staleArtifacts(managed []ManagedArtifact, name string, desired []runtime.Artifact) []ManagedArtifact {
	wanted := make(map[string]bool, len(desired))
	for _, a := range desired {
		wanted[a.Path] = true
	}
	var out []ManagedArtifact
	for _, a := range managed {
		if a.Service == name && !wanted[a.Path] {
			out = append(out, a)
		}
	}
	return out
}

// byLabel groups artifacts by the label whose unit they belong to, preserving
// first-seen order. A runtime that emits several files per Service retires
// them as a group.
func byLabel(arts []ManagedArtifact) ([]string, map[string][]ManagedArtifact) {
	grouped := map[string][]ManagedArtifact{}
	var order []string
	for _, a := range arts {
		if _, seen := grouped[a.Label]; !seen {
			order = append(order, a.Label)
		}
		grouped[a.Label] = append(grouped[a.Label], a)
	}
	return order, grouped
}

// orphans returns managed artifacts whose service is no longer in the Config.
func (m *Manager) orphans(managed []ManagedArtifact) []ManagedArtifact {
	var out []ManagedArtifact
	for _, a := range managed {
		if _, ok := m.Cfg.Service(a.Service); !ok {
			out = append(out, a)
		}
	}
	return out
}

// orphanLabels returns each orphaned label once, with the artifacts that carry
// it.
func (m *Manager) orphanLabels(managed []ManagedArtifact) ([]string, map[string][]ManagedArtifact) {
	return byLabel(m.orphans(managed))
}
