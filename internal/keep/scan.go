package keep

import (
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/launchd"
)

// ManagedArtifact is a generated plist on disk that carries keep's marker.
type ManagedArtifact struct {
	Path     string
	Label    string
	Service  string
	KeepPath string
	Data     []byte
}

// ScanManaged finds every keep-managed plist in the LaunchAgents directory.
// It never reports unmanaged jobs — the marker is the boundary (D2, D19).
func (m *Manager) ScanManaged() ([]ManagedArtifact, error) {
	dir := m.LaunchAgentsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []ManagedArtifact
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".plist" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		marker := launchd.ReadMarkers(data)
		if !marker.Managed {
			continue
		}
		label := e.Name()[:len(e.Name())-len(".plist")]
		found = append(found, ManagedArtifact{
			Path:     path,
			Label:    label,
			Service:  marker.Service,
			KeepPath: marker.KeepPath,
			Data:     data,
		})
	}
	return found, nil
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
