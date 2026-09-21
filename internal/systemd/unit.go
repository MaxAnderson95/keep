// Package systemd is keep's systemd adapter: it renders unit files into the
// user manager's directory, drives `systemctl --user`, and parses its output.
// It is the only package that knows systemd's mechanics; everything above the
// runtime.Runtime seam speaks in Services, not units (ADR-0008).
package systemd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// The marker section keep embeds in every generated unit so it can recognize
// its own artifacts (D19). systemd ignores sections whose name starts with
// "X-" (systemd.unit(5)), so this is inert to the service manager and is the
// systemd equivalent of the plist's KeepManaged keys.
const (
	MarkerSection = "X-Keep"
	MarkerKey     = "Managed"
	ServiceKey    = "Service"
	VersionKey    = "Version"
	KeepPathKey   = "Path"
)

// Unit file extensions this adapter emits.
const (
	serviceExt = ".service"
	timerExt   = ".timer"
)

// scheduled reports whether a Job describes a scheduled Service. The Job says
// so by carrying a schedule; there is no separate flag to trust.
func scheduled(j runtime.Job) bool {
	return j.StartInterval > 0 || len(j.StartCalendar) > 0
}

// Render produces the unit files for a Job: one .service for a resident
// Service, and a .service plus the .timer that drives it for a scheduled one.
// Key order is fixed because apply byte-diffs the result against disk.
func (r *Runtime) Render(j runtime.Job) []runtime.Artifact {
	arts := []runtime.Artifact{{
		Path: filepath.Join(r.ArtifactDir(), j.Label+serviceExt),
		Data: renderService(j),
	}}
	if scheduled(j) {
		arts = append(arts, runtime.Artifact{
			Path: filepath.Join(r.ArtifactDir(), j.Label+timerExt),
			Data: renderTimer(j),
		})
	}
	return arts
}

func renderService(j runtime.Job) []byte {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=keep service %s\n", specifierSafe(j.Service))
	if !scheduled(j) {
		// Paired with Restart=always below: never stop retrying a resident
		// Service, which is what launchd's KeepAlive does. Without this,
		// systemd gives up after 5 restarts in 10s and Start would need a
		// reset-failed first.
		b.WriteString("StartLimitIntervalSec=0\n")
	}

	b.WriteString("\n[Service]\n")
	if scheduled(j) {
		b.WriteString("Type=oneshot\n")
	} else {
		// exec, not simple: a fork that cannot exec the real command is a
		// visible start failure rather than a unit that looks started.
		b.WriteString("Type=exec\n")
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", execLine(j.ProgramArguments))
	if !scheduled(j) {
		b.WriteString("Restart=always\n")
		b.WriteString("RestartSec=10\n")
	}
	if j.StandardOutPath != "" {
		fmt.Fprintf(&b, "StandardOutput=append:%s\n", specifierSafe(j.StandardOutPath))
	}
	if j.StandardErrorPath != "" {
		fmt.Fprintf(&b, "StandardError=append:%s\n", specifierSafe(j.StandardErrorPath))
	}

	// A scheduled Service is started by its timer, so only a resident one is
	// wanted by the session.
	if !scheduled(j) {
		b.WriteString("\n[Install]\n")
		b.WriteString("WantedBy=default.target\n")
	}

	writeMarkers(&b, j)
	return []byte(b.String())
}

func renderTimer(j runtime.Job) []byte {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=keep timer %s\n", specifierSafe(j.Service))

	b.WriteString("\n[Timer]\n")
	for _, ci := range j.StartCalendar {
		fmt.Fprintf(&b, "OnCalendar=%s\n", onCalendar(ci))
	}
	if j.StartInterval > 0 {
		// First fire one interval after the timer starts, then one interval
		// after each run begins. This drifts by the run's own duration, which
		// launchd's StartInterval does not; the approximation is deliberate.
		fmt.Fprintf(&b, "OnActiveSec=%d\n", j.StartInterval)
		fmt.Fprintf(&b, "OnUnitActiveSec=%d\n", j.StartInterval)
	}
	// systemd's default accuracy is a full minute, which is far looser than
	// launchd; Persistent catches up a fire missed while the machine slept.
	b.WriteString("AccuracySec=1s\n")
	b.WriteString("Persistent=true\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=timers.target\n")

	writeMarkers(&b, j)
	return []byte(b.String())
}

func writeMarkers(b *strings.Builder, j runtime.Job) {
	fmt.Fprintf(b, "\n[%s]\n", MarkerSection)
	fmt.Fprintf(b, "%s=true\n", MarkerKey)
	fmt.Fprintf(b, "%s=%s\n", ServiceKey, j.Service)
	if j.KeepVersion != "" {
		fmt.Fprintf(b, "%s=%s\n", VersionKey, j.KeepVersion)
	}
	if j.KeepPath != "" {
		fmt.Fprintf(b, "%s=%s\n", KeepPathKey, j.KeepPath)
	}
}

// weekdayNames maps a launchd weekday number to systemd's day abbreviation.
// launchd accepts both 0 and 7 for Sunday.
var weekdayNames = map[int]string{0: "Sun", 1: "Mon", 2: "Tue", 3: "Wed", 4: "Thu", 5: "Fri", 6: "Sat", 7: "Sun"}

// onCalendar renders one CalendarInterval as a systemd calendar expression. A
// nil field means "every", which is systemd's `*`.
func onCalendar(ci runtime.CalendarInterval) string {
	// The year is always "*": launchd has no year field to carry over.
	stamp := fmt.Sprintf("*-%s-%s %s:%s:00",
		calField(ci.Month), calField(ci.Day),
		calField(ci.Hour), calField(ci.Minute))
	if ci.Weekday == nil {
		return stamp
	}
	day, ok := weekdayNames[*ci.Weekday]
	if !ok {
		// Out of range for a weekday; the Config validator rejects these, so
		// falling back to "every day" is better than emitting a unit systemd
		// will refuse to load.
		return stamp
	}
	return day + " " + stamp
}

func calField(v *int) string {
	if v == nil {
		return "*"
	}
	return fmt.Sprintf("%02d", *v)
}

// execLine renders argv as a systemd ExecStart value. systemd splits the value
// on whitespace, expands `%` specifiers, and substitutes `$VAR` and `${VAR}`
// from the manager's environment — the last two even inside quotes — so
// anything with whitespace is quoted and every literal `%` and `$` is doubled.
func execLine(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, a := range argv {
		quoted = append(quoted, execArg(a))
	}
	return strings.Join(quoted, " ")
}

// execArgEscapes doubles what systemd would otherwise interpret and
// backslash-escapes what would otherwise end the argument. One Replacer, so
// each character is considered once and an escape cannot be re-escaped.
var execArgEscapes = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")

func execArg(a string) string {
	escaped := execArgEscapes.Replace(a)
	if a == "" || strings.ContainsAny(a, " \t'\"\\") {
		return `"` + escaped + `"`
	}
	return escaped
}

// specifierSafe protects a literal value in a unit directive. systemd resolves
// `%` specifiers in most settings, including the paths in
// `StandardOutput=append:`, where an unknown specifier makes it drop the
// directive entirely and fall back to journald — which would send a Service's
// output somewhere `keep logs` never looks.
func specifierSafe(v string) string {
	return strings.ReplaceAll(v, "%", "%%")
}
