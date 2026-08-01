package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/keep"
)

func capture(t *testing.T, render func(*cli.Context)) string {
	t.Helper()
	app := cli.NewApp()
	var buf bytes.Buffer
	app.Writer = &buf
	render(cli.NewContext(app, flag.NewFlagSet("test", flag.ContinueOnError), nil))
	return buf.String()
}

// Optionality (D26): a Config where nothing declares a version_command prints
// exactly the table it always did.
func TestPrintStatusOmitsVersionColumnWhenUnused(t *testing.T) {
	out := capture(t, func(c *cli.Context) {
		printStatus(c, []keep.ServiceStatus{
			{Name: "web", Type: "resident", Health: keep.HealthRunning, PID: 100},
		})
	})
	if strings.Contains(out, "VERSION") {
		t.Fatalf("VERSION column present with no version_command declared:\n%s", out)
	}
}

func TestPrintStatusShowsVersionColumnWhenDeclared(t *testing.T) {
	out := capture(t, func(c *cli.Context) {
		printStatus(c, []keep.ServiceStatus{
			{
				Name: "web", Type: "resident", Health: keep.HealthRunning, PID: 100,
				HasVersionCommand: true, Version: "1.2.3",
			},
			// Declares none: shares the column, but has nothing to put in it.
			{Name: "db", Type: "resident", Health: keep.HealthRunning, PID: 101},
		})
	})
	if !strings.Contains(out, "VERSION") {
		t.Fatalf("VERSION column missing:\n%s", out)
	}
	if !strings.Contains(out, "1.2.3") {
		t.Fatalf("version value missing:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(lines[2], " "), "-") {
		t.Fatalf("service without a version should render a dash:\n%s", out)
	}
}

// A declared version_command with nothing captured yet (service stopped, or
// never restarted since it was declared) renders a dash, not a stale string.
func TestPrintStatusDashesUncapturedVersion(t *testing.T) {
	out := capture(t, func(c *cli.Context) {
		printStatus(c, []keep.ServiceStatus{
			{Name: "web", Type: "resident", Health: keep.HealthStopped, HasVersionCommand: true},
		})
	})
	if !strings.Contains(out, "VERSION") {
		t.Fatalf("VERSION column missing:\n%s", out)
	}
}

func TestPrintShowIncludesVersionCommand(t *testing.T) {
	out := capture(t, func(c *cli.Context) {
		printShow(c, keep.Resolved{
			Name: "web", Type: "resident", Label: "keep.web",
			Argv:           []string{"/usr/bin/true"},
			VersionCommand: "/usr/bin/true --version",
		})
	})
	if !strings.Contains(out, "version_command:") || !strings.Contains(out, "--version") {
		t.Fatalf("version_command missing from show output:\n%s", out)
	}
}

func TestPrintShowOmitsVersionCommandWhenUnset(t *testing.T) {
	out := capture(t, func(c *cli.Context) {
		printShow(c, keep.Resolved{
			Name: "web", Type: "resident", Label: "keep.web",
			Argv: []string{"/usr/bin/true"},
		})
	})
	if strings.Contains(out, "version_command") {
		t.Fatalf("version_command present when unset:\n%s", out)
	}
}
