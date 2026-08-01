package keep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/launchd"
)

// writeScript drops an executable shell script in dir and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestFirstVersionLine(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
		want   string
	}{
		{name: "plain", stdout: "1.2.3\n", want: "1.2.3"},
		{name: "trims whitespace", stdout: "   v20.0.0  \n", want: "v20.0.0"},
		{name: "keeps noisy prefix verbatim", stdout: "git version 2.4.5\n", want: "git version 2.4.5"},
		{name: "first non-empty line only", stdout: "\n\n1.0.0\nbuilt 2026\n", want: "1.0.0"},
		{name: "strips carriage returns", stdout: "1.0.0\r\n", want: "1.0.0"},
		{name: "falls back to stderr", stdout: "  \n", stderr: "tool 9.9\n", want: "tool 9.9"},
		{name: "stdout wins over stderr", stdout: "out 1\n", stderr: "err 2\n", want: "out 1"},
		{name: "nothing at all", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstVersionLine([]byte(tt.stdout), []byte(tt.stderr))
			if got != tt.want {
				t.Fatalf("firstVersionLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstVersionLineTruncates(t *testing.T) {
	got := firstVersionLine([]byte(strings.Repeat("a", maxVersionLen+50)), nil)
	if len(got) != maxVersionLen {
		t.Fatalf("length = %d, want %d", len(got), maxVersionLen)
	}
}

// A multi-byte rune straddling the cut must not leave invalid UTF-8 in the
// cache, which would corrupt the JSON round-trip and every UI that renders it.
func TestFirstVersionLineTruncatesOnRuneBoundary(t *testing.T) {
	got := firstVersionLine([]byte(strings.Repeat("é", maxVersionLen)), nil)
	if !isValidUTF8(got) {
		t.Fatalf("truncated to invalid UTF-8: %q", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

func TestCaptureVersionRecordsOutput(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "app", `echo "app 4.5.6"`)
	cfg := mustParse(t, `
services:
  app:
    command: `+bin+`
    version_command: `+bin+` --version
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	m.CaptureVersion(svc, os.Environ(), 4242)

	entry, ok := m.ReadVersionEntry("app")
	if !ok {
		t.Fatal("no entry written")
	}
	if entry.Version != "app 4.5.6" {
		t.Fatalf("Version = %q, want %q", entry.Version, "app 4.5.6")
	}
	if entry.PID != 4242 {
		t.Fatalf("PID = %d, want 4242", entry.PID)
	}
	if entry.Command != svc.VersionCommand {
		t.Fatalf("Command = %q, want %q", entry.Command, svc.VersionCommand)
	}
	if entry.Error != "" {
		t.Fatalf("Error = %q, want empty", entry.Error)
	}
	if time.Since(entry.CapturedAt) > time.Minute {
		t.Fatalf("CapturedAt = %v, want ~now", entry.CapturedAt)
	}
}

// A tool that prints its version and exits non-zero is common enough that the
// output has to win over the exit code.
func TestCaptureVersionKeepsOutputDespiteNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "app", "echo 'app 1.0'\nexit 3\n")
	cfg := mustParse(t, `
services:
  app:
    command: `+bin+`
    version_command: `+bin+` -v
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	m.CaptureVersion(svc, os.Environ(), 1)

	entry, _ := m.ReadVersionEntry("app")
	if entry.Version != "app 1.0" {
		t.Fatalf("Version = %q, want %q", entry.Version, "app 1.0")
	}
	if entry.Error != "" {
		t.Fatalf("Error = %q, want empty", entry.Error)
	}
}

func TestCaptureVersionRecordsFailure(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: /nonexistent/binary --version
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	m.CaptureVersion(svc, os.Environ(), 7)

	entry, ok := m.ReadVersionEntry("app")
	if !ok {
		t.Fatal("no entry written for a failed capture")
	}
	if entry.Version != "" {
		t.Fatalf("Version = %q, want empty", entry.Version)
	}
	if entry.Error == "" {
		t.Fatal("Error is empty; a failed capture must be recorded for doctor")
	}
}

// The timeout is the guarantee that version capture can never wedge a start.
func TestCaptureVersionTimesOut(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "app", "sleep 30\n")
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: `+bin+`
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	prev := versionCaptureTimeout
	versionCaptureTimeout = 150 * time.Millisecond
	defer func() { versionCaptureTimeout = prev }()

	start := time.Now()
	m.CaptureVersion(svc, os.Environ(), 9)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("capture took %v; the timeout did not fire", elapsed)
	}
	entry, _ := m.ReadVersionEntry("app")
	if entry.Error == "" {
		t.Fatal("a timed-out capture must record an error")
	}
}

func TestCaptureVersionUsesServiceEnv(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "app", `echo "$FLAVOR"`)
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: `+bin+`
    env:
      FLAVOR: from-config
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")
	env, err := cfg.ForkEnv(svc, config.OSEnviron())
	if err != nil {
		t.Fatalf("ForkEnv: %v", err)
	}

	m.CaptureVersion(svc, env, 1)

	entry, _ := m.ReadVersionEntry("app")
	if entry.Version != "from-config" {
		t.Fatalf("Version = %q, want %q", entry.Version, "from-config")
	}
}

// Optionality (D26): a Service without version_command must cost nothing —
// no subprocess, and not even the state directory.
func TestCaptureVersionIsNoOpWithoutVersionCommand(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	m.CaptureVersion(svc, os.Environ(), 1)

	if _, ok := m.ReadVersionEntry("app"); ok {
		t.Fatal("wrote a version entry for a Service that declares no version_command")
	}
	if _, err := os.Stat(m.versionDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("versions directory exists (%v); it must never be created unused", err)
	}
}

func TestLiveVersionRequiresMatchingPIDAndCommand(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: /bin/echo 1.0.0
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")

	tests := []struct {
		name  string
		entry VersionEntry
		pid   int
		want  string
	}{
		{
			name:  "matching pid and command",
			entry: VersionEntry{Version: "1.0.0", PID: 500, Command: svc.VersionCommand},
			pid:   500,
			want:  "1.0.0",
		},
		{
			name:  "restarted since capture",
			entry: VersionEntry{Version: "1.0.0", PID: 499, Command: svc.VersionCommand},
			pid:   500,
			want:  "",
		},
		{
			name:  "captured by a command the Config no longer declares",
			entry: VersionEntry{Version: "1.0.0", PID: 500, Command: "/bin/echo --old"},
			pid:   500,
			want:  "",
		},
		{
			name:  "no live pid",
			entry: VersionEntry{Version: "1.0.0", PID: 500, Command: svc.VersionCommand},
			pid:   0,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := m.writeVersionEntry("app", tt.entry); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := m.LiveVersion(svc, tt.pid); got != tt.want {
				t.Fatalf("LiveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLiveVersionWithoutEntry(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: /bin/echo 1.0.0
`)
	m := testManager(t, cfg, newFakeController())
	svc, _ := cfg.Service("app")
	if got := m.LiveVersion(svc, 500); got != "" {
		t.Fatalf("LiveVersion() = %q, want empty", got)
	}
}

func TestStatusShowsVersionOnlyForTheLiveProcess(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: /bin/echo 1.0.0
`)
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	svc, _ := cfg.Service("app")
	label := svc.EffectiveLabel()

	ctl.loaded[label] = launchd.PrintInfo{Loaded: true, State: "running", PID: 777, HasPID: true}
	if err := m.writeVersionEntry("app", VersionEntry{
		Version: "1.0.0", PID: 777, Command: svc.VersionCommand,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	statuses, err := m.Status(nil)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !statuses[0].HasVersionCommand {
		t.Fatal("HasVersionCommand = false, want true")
	}
	if statuses[0].Version != "1.0.0" {
		t.Fatalf("Version = %q, want %q", statuses[0].Version, "1.0.0")
	}

	// Stopping the Service must hide the version outright — no last-known
	// fallback (ADR-0007).
	delete(ctl.loaded, label)
	statuses, err = m.Status(nil)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if statuses[0].Version != "" {
		t.Fatalf("Version = %q for a stopped Service, want empty", statuses[0].Version)
	}
}

// Optionality (D26): status for a Service without version_command must carry
// no version data at all.
func TestStatusOmitsVersionWithoutVersionCommand(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
`)
	ctl := newFakeController()
	m := testManager(t, cfg, ctl)
	svc, _ := cfg.Service("app")
	ctl.loaded[svc.EffectiveLabel()] = launchd.PrintInfo{
		Loaded: true, State: "running", PID: 777, HasPID: true,
	}

	statuses, err := m.Status(nil)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if statuses[0].HasVersionCommand {
		t.Fatal("HasVersionCommand = true, want false")
	}
	if statuses[0].Version != "" {
		t.Fatalf("Version = %q, want empty", statuses[0].Version)
	}
}

func TestPruneVersionEntries(t *testing.T) {
	cfg := mustParse(t, `
services:
  keeper:
    command: /bin/echo hi
    version_command: /bin/echo 1.0.0
  edited:
    command: /bin/echo hi
    version_command: /bin/echo --new
  dropped:
    command: /bin/echo hi
`)
	m := testManager(t, cfg, newFakeController())
	keeper, _ := cfg.Service("keeper")

	write := func(name, command string) {
		if err := m.writeVersionEntry(name, VersionEntry{
			Version: "1.0.0", PID: 1, Command: command,
		}); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("keeper", keeper.VersionCommand)
	write("edited", "/bin/echo --old")
	write("dropped", "/bin/echo -v")
	write("gone", "/bin/echo -v")

	m.pruneVersionEntries()

	if _, ok := m.ReadVersionEntry("keeper"); !ok {
		t.Error("keeper entry was pruned; its command is unchanged")
	}
	for _, name := range []string{"edited", "dropped", "gone"} {
		if _, ok := m.ReadVersionEntry(name); ok {
			t.Errorf("%s entry survived pruning", name)
		}
	}
}

func TestApplyPrunesStaleVersionEntries(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    command: /usr/bin/true
    version_command: /usr/bin/true --version
`)
	m := testManager(t, cfg, newFakeController())
	if err := m.writeVersionEntry("web", VersionEntry{
		Version: "0.9.0", PID: 1, Command: "/usr/bin/true -v", // the old command
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := m.writeVersionEntry("gone", VersionEntry{Version: "1.0", PID: 1}); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := m.Apply(); err != nil {
		t.Fatal(err)
	}

	if _, ok := m.ReadVersionEntry("web"); ok {
		t.Error("apply kept an entry captured by a command the Config no longer declares")
	}
	if _, ok := m.ReadVersionEntry("gone"); ok {
		t.Error("apply kept an entry for a Service no longer in the Config")
	}
}

func TestPruneVersionEntriesWithNoDirectory(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
`)
	m := testManager(t, cfg, newFakeController())
	m.pruneVersionEntries() // must not panic or create anything
	if _, err := os.Stat(m.versionDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("versions directory exists (%v); pruning must not create it", err)
	}
}

func TestReadVersionEntryIgnoresCorruptFile(t *testing.T) {
	cfg := mustParse(t, `
services:
  app:
    command: /bin/echo hi
    version_command: /bin/echo 1.0.0
`)
	m := testManager(t, cfg, newFakeController())
	if err := os.MkdirAll(m.versionDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(m.versionPath("app"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := m.ReadVersionEntry("app"); ok {
		t.Fatal("corrupt entry reported as valid")
	}
}
