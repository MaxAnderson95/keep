package keep

import (
	"github.com/MaxAnderson95/keep/internal/config"
)

const maskedValue = "********"

// ShownEnv is one assembled env var for `keep show`, with secrets masked.
type ShownEnv struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
	Secret bool   `json:"secret"`
}

// Resolved is the fully-resolved view of a Service (D6, issue #11).
type Resolved struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Label      string     `json:"label"`
	Argv       []string   `json:"argv"`
	WorkingDir string     `json:"working_dir,omitempty"`
	Umask      string     `json:"umask,omitempty"`
	Env        []ShownEnv `json:"env"`
}

// Show resolves a Service's argv and assembled environment, masking secrets
// (values that originate from env_files).
func (m *Manager) Show(name string) (Resolved, error) {
	s, ok := m.Cfg.Service(name)
	if !ok {
		return Resolved{}, errUnknownService(name)
	}
	argv, err := s.ResolveArgv()
	if err != nil {
		return Resolved{}, err
	}
	entries, err := m.Cfg.KeepEnv(s)
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{
		Name:       s.Name,
		Type:       s.Type,
		Label:      s.EffectiveLabel(),
		Argv:       argv,
		WorkingDir: config.ExpandPath(s.WorkingDir),
		Umask:      s.Umask,
		Env:        []ShownEnv{},
	}
	for _, e := range entries {
		val := e.Value
		if e.Secret {
			val = maskedValue
		}
		r.Env = append(r.Env, ShownEnv{
			Key:    e.Key,
			Value:  val,
			Source: string(e.Source),
			Secret: e.Secret,
		})
	}
	return r, nil
}

type unknownServiceError string

func (e unknownServiceError) Error() string { return "unknown service " + string(e) }

func errUnknownService(name string) error { return unknownServiceError(name) }
