package keep

import (
	"bytes"
	"fmt"
	"os"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/launchd"
)

// Severity classifies a doctor finding.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warning"
)

// Finding is a single read-only doctor diagnosis with a suggested fix (D13).
type Finding struct {
	Service  string   `json:"service,omitempty"`
	Severity Severity `json:"severity"`
	Problem  string   `json:"problem"`
	Fix      string   `json:"fix"`
}

// Doctor runs every read-only check across managed Services. It never mutates
// state (D13). The returned findings are empty when everything is healthy.
func (m *Manager) Doctor() ([]Finding, error) {
	var findings []Finding
	managed, err := m.ScanManaged()
	if err != nil {
		return nil, err
	}
	disabled, err := m.ctl.DisabledSet()
	if err != nil {
		return nil, err
	}

	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		label := s.EffectiveLabel()

		// Missing target binary — resolved against the Service's assembled PATH
		// (the same one fork uses), falling back to the ambient PATH if the env
		// can't be assembled (e.g. a broken env_file, reported separately below).
		if argv, err := s.ResolveArgv(); err == nil {
			pathEnv := os.Getenv("PATH")
			if env, eerr := m.Cfg.ForkEnv(s, config.OSEnviron()); eerr == nil {
				pathEnv = pathFromEnv(env)
			}
			if _, rerr := resolveExecutable(argv[0], pathEnv); rerr != nil {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevError,
					Problem:  fmt.Sprintf("target binary %q not found", argv[0]),
					Fix:      "install the binary or correct the command/args path",
				})
			}
		}

		// Broken env_file references.
		for _, ref := range m.Cfg.EnvFileRefs(s) {
			if _, serr := os.Stat(ref); serr != nil {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevError,
					Problem:  fmt.Sprintf("env_file not found: %s", ref),
					Fix:      "create the env_file or remove the reference from the Config",
				})
			}
		}

		// Plist presence / hand-edit / stale path.
		path := m.PlistPath(s)
		desired, derr := m.PlistBytes(s)
		if derr != nil {
			return nil, derr
		}
		existing, rerr := os.ReadFile(path)
		switch {
		case os.IsNotExist(rerr):
			if s.IsEnabled() {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "no generated artifact on disk",
					Fix:      "run `keep apply`",
				})
			}
		case rerr == nil:
			if !bytes.Equal(existing, desired) {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "generated artifact differs from Config (hand-edited or stale)",
					Fix:      "run `keep apply` to regenerate it",
				})
			}
			if kp := launchd.ReadMarkers(existing).KeepPath; kp != "" && kp != m.KeepPath {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  fmt.Sprintf("artifact pins a stale keep path %q (current: %q)", kp, m.KeepPath),
					Fix:      "run `keep apply` to re-pin the current keep binary path",
				})
			}
		}

		// Error / drift states from live launchd.
		info, ierr := m.ctl.Info(label)
		if ierr == nil {
			if info.Loaded && info.HasLastExit && info.LastExit != 0 {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  fmt.Sprintf("last exit code was %d", info.LastExit),
					Fix:      "check `keep logs " + s.Name + "` for the failure",
				})
			}
			if s.IsEnabled() && !disabled[label] && !info.Loaded {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "declared enabled but not loaded in launchd",
					Fix:      "run `keep apply` or `keep up " + s.Name + "`",
				})
			}
		}
		if s.IsEnabled() && disabled[label] {
			findings = append(findings, Finding{
				Service:  s.Name,
				Severity: SevWarn,
				Problem:  "held down (declared enabled, currently disabled)",
				Fix:      "run `keep up " + s.Name + "` to release the hold",
			})
		}
	}

	// Orphaned managed artifacts.
	for _, a := range m.orphans(managed) {
		findings = append(findings, Finding{
			Service:  a.Service,
			Severity: SevWarn,
			Problem:  fmt.Sprintf("orphaned managed artifact %s (not in Config)", a.Path),
			Fix:      "run `keep apply` to prune it",
		})
	}
	return findings, nil
}
