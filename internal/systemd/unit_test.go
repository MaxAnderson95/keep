package systemd

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

var updateGoldens = flag.Bool("update-goldens", false, "rewrite the unit golden files")

func intp(i int) *int { return &i }

func residentJob() runtime.Job {
	return runtime.Job{
		Label:             "keep.web",
		ProgramArguments:  []string{"/opt/keep/bin/keep", "fork", "web"},
		RunAtLoad:         true,
		KeepAlive:         true,
		StandardOutPath:   "/home/m/.local/state/keep/logs/web.out.log",
		StandardErrorPath: "/home/m/.local/state/keep/logs/web.err.log",
		Service:           "web",
		KeepVersion:       "1.2.3",
		KeepPath:          "/opt/keep/bin/keep",
	}
}

func intervalJob() runtime.Job {
	return runtime.Job{
		Label:             "keep.backup",
		ProgramArguments:  []string{"/opt/keep/bin/keep", "fork", "backup"},
		StandardOutPath:   "/home/m/.local/state/keep/logs/backup.out.log",
		StandardErrorPath: "/home/m/.local/state/keep/logs/backup.err.log",
		StartInterval:     21600,
		Service:           "backup",
		KeepVersion:       "1.2.3",
		KeepPath:          "/opt/keep/bin/keep",
	}
}

func calendarJob() runtime.Job {
	return runtime.Job{
		Label:             "keep.nightly",
		ProgramArguments:  []string{"/opt/keep/bin/keep", "--config", "/home/m/my keep.yaml", "fork", "nightly"},
		StandardOutPath:   "/home/m/.local/state/keep/logs/nightly.out.log",
		StandardErrorPath: "/home/m/.local/state/keep/logs/nightly.err.log",
		StartCalendar: []runtime.CalendarInterval{
			{Hour: intp(2), Minute: intp(30)},                               // every day, no weekday
			{Weekday: intp(0), Hour: intp(4), Minute: intp(0)},              // Sunday as 0
			{Weekday: intp(7), Hour: intp(5), Minute: intp(15)},             // Sunday as 7
			{Month: intp(6), Day: intp(1), Hour: intp(6), Minute: intp(45)}, // one date a year
		},
		Service:     "nightly",
		KeepVersion: "1.2.3",
		KeepPath:    "/opt/keep/bin/keep",
	}
}

// The units are what systemd actually reads, and apply byte-diffs them against
// disk, so every rendered byte is pinned here. Regenerate deliberately with
// `go test ./internal/systemd -update-goldens` and read the diff.
func TestRenderGoldens(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	cases := map[string]runtime.Job{
		"resident": residentJob(),
		"interval": intervalJob(),
		"calendar": calendarJob(),
	}
	for name, job := range cases {
		t.Run(name, func(t *testing.T) {
			for _, a := range r.Render(job) {
				golden := filepath.Join("testdata", name+filepath.Ext(a.Path))
				if *updateGoldens {
					if err := os.WriteFile(golden, a.Data, 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if string(a.Data) != string(want) {
					t.Errorf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", golden, a.Data, want)
				}
			}
		})
	}
}

func TestRenderArtifactCounts(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	if arts := r.Render(residentJob()); len(arts) != 1 || arts[0].Path != "/units/keep.web.service" {
		t.Errorf("a resident Service needs one .service, got %+v", arts)
	}
	arts := r.Render(intervalJob())
	if len(arts) != 2 {
		t.Fatalf("a scheduled Service needs a .service and a .timer, got %d", len(arts))
	}
	if arts[0].Path != "/units/keep.backup.service" || arts[1].Path != "/units/keep.backup.timer" {
		t.Errorf("paths = %q, %q", arts[0].Path, arts[1].Path)
	}
}

// A scheduled Service is started by its timer, so enabling it must enable the
// timer, and only the timer can carry [Install].
func TestScheduledServiceHasNoInstallSection(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	arts := r.Render(intervalJob())
	if strings.Contains(string(arts[0].Data), "[Install]") {
		t.Errorf("the scheduled .service must not be enableable:\n%s", arts[0].Data)
	}
	if !strings.Contains(string(arts[1].Data), "WantedBy=timers.target") {
		t.Errorf("the .timer must be wanted by timers.target:\n%s", arts[1].Data)
	}
	if strings.Contains(string(arts[0].Data), "Restart=") {
		t.Errorf("a scheduled run must not be restarted:\n%s", arts[0].Data)
	}
}

// Paired with Restart=always, this is what makes a crash-looping resident
// Service behave like it does under launchd's KeepAlive instead of systemd
// giving up after five tries.
func TestResidentNeverGivesUp(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	out := string(r.Render(residentJob())[0].Data)
	for _, want := range []string{"StartLimitIntervalSec=0", "Restart=always", "RestartSec=10", "Type=exec"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestOnCalendarMapping(t *testing.T) {
	cases := []struct {
		name string
		in   runtime.CalendarInterval
		want string
	}{
		{"every day", runtime.CalendarInterval{Hour: intp(2), Minute: intp(30)}, "*-*-* 02:30:00"},
		{"weekday 0 is Sunday", runtime.CalendarInterval{Weekday: intp(0), Hour: intp(4), Minute: intp(0)}, "Sun *-*-* 04:00:00"},
		{"weekday 7 is Sunday too", runtime.CalendarInterval{Weekday: intp(7), Hour: intp(5), Minute: intp(15)}, "Sun *-*-* 05:15:00"},
		{"weekday 6 is Saturday", runtime.CalendarInterval{Weekday: intp(6), Hour: intp(1), Minute: intp(2)}, "Sat *-*-* 01:02:00"},
		{"date only", runtime.CalendarInterval{Month: intp(6), Day: intp(1)}, "*-06-01 *:*:00"},
		{"everything nil", runtime.CalendarInterval{}, "*-*-* *:*:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := onCalendar(tc.in); got != tc.want {
				t.Errorf("onCalendar = %q, want %q", got, tc.want)
			}
		})
	}
}

// systemd splits ExecStart on whitespace and expands % specifiers, so a config
// path with a space or a literal % has to survive the round trip.
func TestExecLineQuoting(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"/opt/keep", "fork", "web"}, "/opt/keep fork web"},
		{[]string{"/opt/keep", "--config", "/home/m/my keep.yaml"}, `/opt/keep --config "/home/m/my keep.yaml"`},
		{[]string{"/opt/keep", "fork", "100%"}, "/opt/keep fork 100%%"},
		{[]string{"/opt/keep", `a"b`}, `/opt/keep "a\"b"`},
	}
	for _, tc := range cases {
		if got := execLine(tc.in); got != tc.want {
			t.Errorf("execLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadMarkers(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	data := r.Render(residentJob())[0].Data

	m := r.ReadMarkers("/units/keep.web.service", data)
	if !m.Managed {
		t.Fatalf("generated unit not detected as managed:\n%s", data)
	}
	if m.Label != "keep.web" {
		t.Errorf("Label = %q, want keep.web (from the filename stem)", m.Label)
	}
	if m.Service != "web" {
		t.Errorf("Service = %q, want web", m.Service)
	}
	if m.KeepPath != "/opt/keep/bin/keep" {
		t.Errorf("KeepPath = %q", m.KeepPath)
	}

	// The timer of a scheduled Service carries the same markers, so both of
	// its files belong to one Service.
	timer := r.Render(intervalJob())[1]
	if tm := r.ReadMarkers(timer.Path, timer.Data); !tm.Managed || tm.Label != "keep.backup" || tm.Service != "backup" {
		t.Errorf("timer markers = %+v", tm)
	}
}

func TestReadMarkersRejectsForeignFiles(t *testing.T) {
	r := &Runtime{artifactDir: "/units"}
	unmanaged := []byte("[Unit]\nDescription=someone else's unit\n\n[Service]\nExecStart=/usr/bin/true\n")
	if m := r.ReadMarkers("/units/vendor.service", unmanaged); m.Managed {
		t.Error("a unit without the marker section must never be managed")
	}
	// The user unit directory holds more than units.
	managed := r.Render(residentJob())[0].Data
	if m := r.ReadMarkers("/units/keep.web.conf", managed); m.Managed {
		t.Error("only .service and .timer files are this adapter's output")
	}
	// A Managed key outside the [X-Keep] section is somebody else's key.
	decoy := []byte("[Service]\nManaged=true\nService=web\n")
	if m := r.ReadMarkers("/units/decoy.service", decoy); m.Managed {
		t.Error("the marker only counts inside the [X-Keep] section")
	}
}

// systemd substitutes $VAR and ${VAR} from the manager's environment even
// inside quotes, so a Config path containing a literal dollar would become a
// different path (or an empty one) before keep fork ever saw it.
func TestExecLineEscapesDollars(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"/opt/keep", "--config", "/home/m/${PROFILE}.yaml"}, `/opt/keep --config /home/m/$${PROFILE}.yaml`},
		{[]string{"/opt/keep", "--config", "/home/m/$HOME.yaml"}, `/opt/keep --config /home/m/$$HOME.yaml`},
		{[]string{"/opt/keep", "--config", "/home/m/my $dir/k.yaml"}, `/opt/keep --config "/home/m/my $$dir/k.yaml"`},
	}
	for _, tc := range cases {
		if got := execLine(tc.in); got != tc.want {
			t.Errorf("execLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// systemd resolves % specifiers in directive values, and an unknown one makes
// it drop the whole directive — which would silently send a Service's output
// to journald, where `keep logs` never looks.
func TestLogPathsEscapePercentSpecifiers(t *testing.T) {
	j := residentJob()
	j.StandardOutPath = "/home/m/logs/%n/web.out.log"
	j.StandardErrorPath = "/home/m/logs/100%/web.err.log"
	j.Service = "we%b"
	out := string((&Runtime{artifactDir: "/units"}).Render(j)[0].Data)

	for _, want := range []string{
		"StandardOutput=append:/home/m/logs/%%n/web.out.log",
		"StandardError=append:/home/m/logs/100%%/web.err.log",
		"Description=keep service we%%b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// keep reads its own markers back and compares the pinned path against the
// running binary, so those values must stay literal. systemd ignores the
// whole X- section, so there is nothing to escape them for.
func TestMarkerValuesAreNotEscaped(t *testing.T) {
	j := residentJob()
	j.KeepPath = "/opt/100%/keep"
	r := &Runtime{artifactDir: "/units"}
	art := r.Render(j)[0]
	if got := r.ReadMarkers(art.Path, art.Data).KeepPath; got != j.KeepPath {
		t.Errorf("KeepPath round-tripped as %q, want %q", got, j.KeepPath)
	}
}

// systemd resolves % specifiers across the whole command line but substitutes
// variables only in the arguments, so the executable and its arguments need
// different escaping. Both halves verified against systemd 255: an unescaped
// `%` in the binary path expanded, and a doubled `$` there was not undone,
// each failing the unit with status 203.
func TestExecLineEscapesTheExecutableDifferently(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{
			name: "a dollar in the binary path stays single",
			in:   []string{"/home/m/t$dir/keep", "fork", "web"},
			want: `/home/m/t$dir/keep fork web`,
		},
		{
			name: "a percent in the binary path is still doubled",
			in:   []string{"/home/m/t%dir/keep", "fork", "web"},
			want: `/home/m/t%%dir/keep fork web`,
		},
		{
			name: "arguments double both",
			in:   []string{"/opt/keep", "--config", "/home/m/$d/100%.yaml"},
			want: `/opt/keep --config /home/m/$$d/100%%.yaml`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := execLine(tc.in); got != tc.want {
				t.Errorf("execLine = %q, want %q", got, tc.want)
			}
		})
	}
}
