package keep

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/runtime"
)

// Severity classifies a doctor finding.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warning"
	SevInfo  Severity = "info"
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
	// Whatever is wrong with the runtime environment itself explains every
	// per-Service symptom below it, so it is diagnosed first — and before any
	// query that needs the runtime to answer. A doctor that failed with a raw
	// "cannot reach the service manager" error would withhold the one finding
	// that says how to fix exactly that.
	findings := m.runtimeFindings()

	managed, err := m.ScanManaged()
	if err != nil {
		return findings, err
	}
	held, err := m.rt.Held()
	if err != nil {
		if len(findings) > 0 {
			// The adapter already explained why the runtime is unusable, with
			// a fix. That diagnosis is the answer; the error underneath it is
			// the symptom.
			return findings, nil
		}
		return findings, err
	}

	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		label := s.EffectiveLabel()

		// Binaries are resolved against the Service's assembled PATH (the same
		// one fork uses), falling back to the ambient PATH if the env can't be
		// assembled (e.g. a broken env_file, reported separately below).
		pathEnv := os.Getenv("PATH")
		if env, eerr := m.Cfg.ForkEnv(s, config.OSEnviron()); eerr == nil {
			pathEnv = pathFromEnv(env)
		}

		// Missing target binary.
		if argv, err := s.ResolveArgv(); err == nil {
			if _, rerr := resolveExecutable(argv[0], pathEnv); rerr != nil {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevError,
					Problem:  fmt.Sprintf("target binary %q not found", argv[0]),
					Fix:      "install the binary or correct the command/args path",
				})
			}
		}

		// Missing version_command binary. Capture failures are silent at fork
		// time by design (ADR-0007), so doctor is where a typo'd path surfaces
		// without waiting for the next restart.
		if s.HasVersionCommand() {
			if argv, err := s.ResolveVersionArgv(); err == nil {
				if _, rerr := resolveExecutable(argv[0], pathEnv); rerr != nil {
					findings = append(findings, Finding{
						Service:  s.Name,
						Severity: SevWarn,
						Problem:  fmt.Sprintf("version_command binary %q not found", argv[0]),
						Fix:      "correct version_command, or remove it if the tool reports no version",
					})
				}
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

		// Artifact presence / hand-edit / stale path.
		desired, derr := m.Artifacts(s)
		if derr != nil {
			return nil, derr
		}
		findings = append(findings, m.artifactFindings(s, desired)...)

		// Error / drift states from the live runtime.
		info, ierr := m.rt.Info(m.target(s))
		if ierr == nil {
			if info.Known() && failedExit(info) {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  fmt.Sprintf("last exit code was %d", *info.LastExit),
					Fix:      "check `keep logs " + s.Name + "` for the failure",
				})
			}
			if s.IsEnabled() && !held[label] && !info.Known() {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "declared enabled but not loaded in the runtime",
					Fix:      "run `keep apply` or `keep up " + s.Name + "`",
				})
			}
			findings = append(findings, m.versionCaptureFindings(s, info)...)
		}
		if s.IsEnabled() && held[label] {
			findings = append(findings, Finding{
				Service:  s.Name,
				Severity: SevWarn,
				Problem:  "held down (declared enabled, currently disabled)",
				Fix:      "run `keep up " + s.Name + "` to release the hold",
			})
		}
	}

	findings = append(findings, m.orphanFindings(managed)...)
	return findings, nil
}

// runtimeFindings asks the adapter what is wrong with the environment it needs
// — a missing systemd user session, lingering left off. An adapter with
// nothing OS-specific to check (launchd) implements no Diagnoser and
// contributes nothing.
func (m *Manager) runtimeFindings() []Finding {
	d, ok := m.rt.(runtime.Diagnoser)
	if !ok {
		return nil
	}
	var findings []Finding
	for _, diag := range d.Diagnose() {
		findings = append(findings, Finding{
			Severity: Severity(diag.Severity),
			Problem:  diag.Problem,
			Fix:      diag.Fix,
		})
	}
	return findings
}

// artifactFindings checks each file the runtime renders for a Service against
// what is on disk.
func (m *Manager) artifactFindings(s *config.Service, desired []runtime.Artifact) []Finding {
	var findings []Finding
	for _, a := range desired {
		existing, rerr := os.ReadFile(a.Path)
		switch {
		case os.IsNotExist(rerr):
			if s.IsEnabled() {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "no generated artifact on disk: " + filepath.Base(a.Path),
					Fix:      "run `keep apply`",
				})
			}
		case rerr == nil:
			if !bytes.Equal(existing, a.Data) {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  "generated artifact differs from Config (hand-edited or stale): " + filepath.Base(a.Path),
					Fix:      "run `keep apply` to regenerate it",
				})
			}
			if kp := m.rt.ReadMarkers(a.Path, existing).KeepPath; kp != "" && kp != m.KeepPath {
				findings = append(findings, Finding{
					Service:  s.Name,
					Severity: SevWarn,
					Problem:  fmt.Sprintf("artifact pins a stale keep path %q (current: %q)", kp, m.KeepPath),
					Fix:      "run `keep apply` to re-pin the current keep binary path",
				})
			}
		}
	}
	return findings
}

// versionCaptureFindings reports what the last start recorded for a Service's
// version_command (D26). It returns nothing at all for a Service that declares
// none, and nothing for one that is not currently running — a stopped Service
// has no live version to be missing.
func (m *Manager) versionCaptureFindings(s *config.Service, info runtime.Info) []Finding {
	if !s.HasVersionCommand() || !info.Running() {
		return nil
	}
	entry, found := m.ReadVersionEntry(s.Name)
	current := found && entry.PID == info.PID && entry.Command == s.VersionCommand

	if current && entry.Error != "" {
		return []Finding{{
			Service:  s.Name,
			Severity: SevWarn,
			Problem:  "version_command failed at the last start: " + entry.Error,
			Fix:      "run the command by hand to see why, or correct version_command",
		}}
	}
	if current && entry.Version != "" {
		return nil
	}
	// Declaring or editing version_command changes no artifact, so `apply`
	// never restarts anything to pick it up.
	return []Finding{{
		Service:  s.Name,
		Severity: SevInfo,
		Problem:  "version_command declared but no version captured for the running process",
		Fix:      "run `keep bounce " + s.Name + "` to capture it at the next start",
	}}
}

func (m *Manager) orphanFindings(managed []ManagedArtifact) []Finding {
	var findings []Finding
	// Orphaned managed artifacts.
	for _, a := range m.orphans(managed) {
		findings = append(findings, Finding{
			Service:  a.Service,
			Severity: SevWarn,
			Problem:  fmt.Sprintf("orphaned managed artifact %s (not in Config)", a.Path),
			Fix:      "run `keep apply` to prune it",
		})
	}
	return findings
}
