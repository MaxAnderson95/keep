package systemd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// Probe reports whether the systemd user manager is actually reachable.
//
// It asks for the manager's environment rather than its version: `systemctl
// --user --version` prints a version without ever contacting the user bus, so
// it succeeds on a machine where no user session exists and would say nothing
// about whether keep can drive anything.
func (r *Runtime) Probe(ctx context.Context) error {
	if out, err := r.run(ctx, "show-environment"); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return nil
}

// Diagnose reports the environment problems that stop the systemd adapter from
// working, or make it behave in a way the user did not intend. Read-only: it
// never enables lingering or starts anything (D13).
func (r *Runtime) Diagnose() []runtime.Diagnosis {
	ctx := context.Background()
	if err := r.Probe(ctx); err != nil {
		// Everything below talks to the same manager, so there is nothing
		// useful to add until this is fixed.
		return []runtime.Diagnosis{{
			Severity: runtime.SevError,
			Problem:  "systemctl --user is not reachable: " + err.Error(),
			Fix:      "log in as this user, or run `loginctl enable-linger " + username() + "` and export XDG_RUNTIME_DIR",
		}}
	}

	var out []runtime.Diagnosis
	if v := r.version(ctx); v > 0 && v < MinVersion {
		out = append(out, runtime.Diagnosis{
			Severity: runtime.SevError,
			Problem: fmt.Sprintf("systemd %d is too old: keep writes Service logs with StandardOutput=append:, which needs systemd %d or newer",
				v, MinVersion),
			Fix: "upgrade systemd, or run keep on a newer distribution release",
		})
	}
	if !r.lingering(ctx) {
		out = append(out, runtime.Diagnosis{
			Severity: runtime.SevWarning,
			Problem:  "lingering is off for this user, so Services stop at logout and do not start at boot",
			Fix:      "run `loginctl enable-linger " + username() + "` (keep never changes this for you)",
		})
	}
	return out
}

func (r *Runtime) version(ctx context.Context) int {
	out, err := r.run(ctx, "--version")
	if err != nil {
		return 0
	}
	return parseVersion(out)
}

// lingering reports whether logind keeps this user's manager running without a
// session. An unreadable answer counts as lingering: doctor should not invent
// a warning it cannot support.
func (r *Runtime) lingering(ctx context.Context) bool {
	out, err := r.loginctl(ctx, "show-user", username(), "--property=Linger")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) == "Linger=yes"
}

func execLoginctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "loginctl", args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
