package launchd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

// The goldens were generated from the renderer as it stood before the runtime
// seam existed. They exist so this refactor cannot change a single byte of what
// lands in ~/Library/LaunchAgents: the first `keep apply` after upgrading must
// be a no-op, and a byte change would reload every Service, including the
// `keep serve` instance driving the apply.
func TestRenderMatchesPreSeamGoldens(t *testing.T) {
	ptr := func(n int) *int { return &n }
	cases := map[string]runtime.Job{
		"resident": {
			Label:             "keep.web",
			ProgramArguments:  []string{"/opt/keep/bin/keep", "--config", "/home/m/keep & co.yaml", "fork", "web"},
			RunAtLoad:         true,
			KeepAlive:         true,
			StandardOutPath:   "/home/m/logs/web.out.log",
			StandardErrorPath: "/home/m/logs/web.err.log",
			Service:           "web",
			KeepVersion:       "1.2.3",
			KeepPath:          "/opt/keep/bin/keep",
		},
		"interval": {
			Label:             "keep.backup",
			ProgramArguments:  []string{"/opt/keep/bin/keep", "fork", "backup"},
			StandardOutPath:   "/home/m/logs/backup.out.log",
			StandardErrorPath: "/home/m/logs/backup.err.log",
			StartInterval:     3600,
			Service:           "backup",
			KeepVersion:       "1.2.3",
			KeepPath:          "/opt/keep/bin/keep",
		},
		"calendar": {
			Label:             "keep.nightly",
			ProgramArguments:  []string{"/opt/keep/bin/keep", "fork", "nightly"},
			StandardOutPath:   "/home/m/logs/nightly.out.log",
			StandardErrorPath: "/home/m/logs/nightly.err.log",
			StartCalendar: []runtime.CalendarInterval{
				{Hour: ptr(2), Minute: ptr(30)},
				{Weekday: ptr(0), Hour: ptr(4), Day: ptr(1), Month: ptr(6)},
			},
			Service:     "nightly",
			KeepVersion: "1.2.3",
			KeepPath:    "/opt/keep/bin/keep",
		},
	}

	r := &Runtime{artifactDir: "/agents"}
	for name, job := range cases {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name+".plist"))
			if err != nil {
				t.Fatal(err)
			}
			arts := r.Render(job)
			if len(arts) != 1 {
				t.Fatalf("launchd renders one artifact per Service, got %d", len(arts))
			}
			if got := filepath.Join("/agents", job.Label+".plist"); arts[0].Path != got {
				t.Errorf("Path = %q, want %q", arts[0].Path, got)
			}
			if string(arts[0].Data) != string(want) {
				t.Errorf("rendered bytes differ from the pre-seam golden:\n--- got ---\n%s\n--- want ---\n%s",
					arts[0].Data, want)
			}
		})
	}
}
