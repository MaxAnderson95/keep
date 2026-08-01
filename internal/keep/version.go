package keep

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
)

// versionCaptureTimeout bounds one version capture (D26, ADR-0007). Fixed, not
// configurable: a version command slower than this is misconfigured, and the
// bound is paid on every start of every versioned Service. Variable for tests.
var versionCaptureTimeout = 5 * time.Second

// maxVersionLen caps the recorded version string. Version output is a line,
// not a document; a runaway command must not write an unbounded cache file.
const maxVersionLen = 200

// VersionEntry is one Service's cached version capture, written by `keep fork`
// immediately before it execs the real command.
//
// PID is the PID fork is about to become (exec preserves it), so an entry is
// only about the process that is still running if PID matches the live one.
// Command is the version_command that produced Version, so an entry produced
// by a command the Config no longer declares can be recognized and ignored.
type VersionEntry struct {
	Version    string    `json:"version,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
	PID        int       `json:"pid"`
	Command    string    `json:"command"`
	Error      string    `json:"error,omitempty"`
}

// versionDir holds one JSON file per Service. Per-service files mean the
// simultaneous forks launchd triggers at login never contend (ADR-0007).
func (m *Manager) versionDir() string {
	return filepath.Join(m.StateDir(), "versions")
}

func (m *Manager) versionPath(name string) string {
	return filepath.Join(m.versionDir(), name+".json")
}

// ReadVersionEntry returns the Service's cached capture. A missing, unreadable,
// or corrupt file is simply "no entry" — this is display metadata, never a
// reason to fail a command.
func (m *Manager) ReadVersionEntry(name string) (VersionEntry, bool) {
	data, err := os.ReadFile(m.versionPath(name))
	if err != nil {
		return VersionEntry{}, false
	}
	var e VersionEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return VersionEntry{}, false
	}
	return e, true
}

// LiveVersion returns the version to display for a running Service: the cached
// string, but only when the entry belongs to the live process (PID match) and
// was produced by the version_command the Config declares right now. Anything
// else returns "" — there is deliberately no last-known fallback (ADR-0007).
func (m *Manager) LiveVersion(s *config.Service, pid int) string {
	if !s.HasVersionCommand() || pid <= 0 {
		return ""
	}
	e, ok := m.ReadVersionEntry(s.Name)
	if !ok || e.PID != pid || e.Command != s.VersionCommand {
		return ""
	}
	return e.Version
}

// writeVersionEntry persists an entry atomically (temp + rename).
func (m *Manager) writeVersionEntry(name string, e VersionEntry) error {
	if err := os.MkdirAll(m.versionDir(), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := m.versionPath(name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// removeVersionEntry drops a Service's cached capture. Missing is success.
func (m *Manager) removeVersionEntry(name string) error {
	if err := os.Remove(m.versionPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// pruneVersionEntries drops entries that can no longer be displayed: Services
// the Config no longer declares, Services that dropped or edited their
// version_command, and Services that never had one. Called from apply so a
// string produced by a command you have since changed is never shown.
//
// A Config where nothing declares a version_command leaves the directory
// untouched (and, having never been created, absent).
func (m *Manager) pruneVersionEntries() {
	entries, err := os.ReadDir(m.versionDir())
	if err != nil {
		return // no directory: nothing was ever captured
	}
	declared := map[string]string{}
	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		if s.HasVersionCommand() {
			declared[s.Name] = s.VersionCommand
		}
	}
	for _, de := range entries {
		name, ok := strings.CutSuffix(de.Name(), ".json")
		if de.IsDir() || !ok {
			continue
		}
		cmd, stillDeclared := declared[name]
		if !stillDeclared {
			_ = m.removeVersionEntry(name)
			continue
		}
		if e, found := m.ReadVersionEntry(name); found && e.Command != cmd {
			_ = m.removeVersionEntry(name)
		}
	}
}

// CaptureVersion runs the Service's version_command and records the result for
// pid — the PID the calling fork process is about to become. It is a no-op for
// a Service that declares no version_command.
//
// It never reports failure, by design: a version string is display metadata,
// and nothing about capturing it may stop or delay a Service beyond the
// timeout. Failures are recorded in the entry instead, where `keep doctor`
// surfaces them (ADR-0007).
func (m *Manager) CaptureVersion(s *config.Service, env []string, pid int) {
	if !s.HasVersionCommand() {
		return
	}
	e := VersionEntry{
		CapturedAt: time.Now().UTC(),
		PID:        pid,
		Command:    s.VersionCommand,
	}
	version, err := m.runVersionCommand(s, env)
	if err != nil {
		e.Error = err.Error()
	}
	e.Version = version
	_ = m.writeVersionEntry(s.Name, e)
}

// runVersionCommand executes the version_command with the Service's assembled
// environment and working directory, capturing output into memory so it never
// reaches the Service's logs. On timeout the whole process group is killed.
func (m *Manager) runVersionCommand(s *config.Service, env []string) (string, error) {
	argv, err := s.ResolveVersionArgv()
	if err != nil {
		return "", err
	}
	bin, err := resolveExecutable(argv[0], pathFromEnv(env))
	if err != nil {
		return "", err
	}
	argv[0] = bin

	ctx, cancel := context.WithTimeout(context.Background(), versionCaptureTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := &exec.Cmd{
		Path:        bin,
		Args:        argv,
		Env:         env,
		Stdout:      &stdout,
		Stderr:      &stderr,
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	if s.WorkingDir != "" {
		cmd.Dir = config.ExpandPath(s.WorkingDir)
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		pgid := cmd.Process.Pid // == pgid, thanks to Setpgid
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
		return "", context.DeadlineExceeded
	}

	// Some tools print their version and exit non-zero. A usable line is worth
	// more than the exit code, so the output wins when there is one.
	line := firstVersionLine(stdout.Bytes(), stderr.Bytes())
	if line != "" {
		return line, nil
	}
	if runErr != nil {
		return "", runErr
	}
	return "", errNoVersionOutput
}

type versionError string

func (e versionError) Error() string { return string(e) }

const errNoVersionOutput = versionError("version_command produced no output")

// firstVersionLine reduces captured output to one displayable string: the first
// non-empty line, trimmed and capped. stdout wins; stderr is the fallback for
// the tools that print their version there (ADR-0007).
func firstVersionLine(stdout, stderr []byte) string {
	if line := firstNonEmptyLine(stdout); line != "" {
		return line
	}
	return firstNonEmptyLine(stderr)
}

func firstNonEmptyLine(b []byte) string {
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(strings.ReplaceAll(raw, "\r", ""))
		if line == "" {
			continue
		}
		if len(line) > maxVersionLen {
			// Cut on bytes, then drop a rune the cut may have split.
			line = strings.ToValidUTF8(line[:maxVersionLen], "")
		}
		return line
	}
	return ""
}
