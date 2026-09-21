package launchd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

func intp(i int) *int { return &i }

func residentJob() runtime.Job {
	return runtime.Job{
		Label:             "keep.web",
		ProgramArguments:  []string{"/Users/me/.local/bin/keep", "fork", "web"},
		RunAtLoad:         true,
		KeepAlive:         true,
		StandardOutPath:   "/Users/me/Library/Logs/keep/web.out.log",
		StandardErrorPath: "/Users/me/Library/Logs/keep/web.err.log",
		Service:           "web",
		KeepVersion:       "1.2.3",
		KeepPath:          "/Users/me/.local/bin/keep",
	}
}

func TestRenderDeterministic(t *testing.T) {
	a := renderPlist(residentJob())
	b := renderPlist(residentJob())
	if string(a) != string(b) {
		t.Fatal("Render is not deterministic")
	}
}

func TestRenderResidentContents(t *testing.T) {
	out := string(renderPlist(residentJob()))
	wantContains := []string{
		"<key>Label</key>",
		"<string>keep.web</string>",
		"<key>ProgramArguments</key>",
		"<string>/Users/me/.local/bin/keep</string>",
		"<string>fork</string>",
		"<string>web</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<true/>",
		"<key>KeepManaged</key>",
		"<key>KeepService</key>",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("rendered plist missing %q\n%s", w, out)
		}
	}
	// Secrets must never be present.
	if strings.Contains(out, "EnvironmentVariables") {
		t.Error("plist must not carry EnvironmentVariables")
	}
}

func TestRenderScheduledInterval(t *testing.T) {
	j := runtime.Job{
		Label:            "keep.backup",
		ProgramArguments: []string{"/keep", "fork", "backup"},
		StartInterval:    21600,
		Service:          "backup",
	}
	out := string(renderPlist(j))
	if !strings.Contains(out, "<key>StartInterval</key>") || !strings.Contains(out, "<integer>21600</integer>") {
		t.Errorf("missing StartInterval:\n%s", out)
	}
	if strings.Contains(out, "RunAtLoad") || strings.Contains(out, "KeepAlive") {
		t.Error("scheduled job must not have RunAtLoad/KeepAlive")
	}
}

func TestRenderScheduledCalendar(t *testing.T) {
	j := runtime.Job{
		Label:            "keep.backup",
		ProgramArguments: []string{"/keep", "fork", "backup"},
		StartCalendar: []runtime.CalendarInterval{
			{Hour: intp(2), Minute: intp(30)},
			{Hour: intp(14), Minute: intp(0)},
		},
		Service: "backup",
	}
	out := string(renderPlist(j))
	if !strings.Contains(out, "<key>StartCalendarInterval</key>") {
		t.Fatalf("missing StartCalendarInterval:\n%s", out)
	}
	// Two calendar entries (each carries an Hour key).
	if strings.Count(out, "<key>Hour</key>") != 2 {
		t.Errorf("want 2 calendar entries, got:\n%s", out)
	}
	if !strings.Contains(out, "<key>Minute</key>\n\t\t\t<integer>0</integer>") {
		t.Errorf("Minute 0 should render:\n%s", out)
	}
}

func TestMarkerExtraction(t *testing.T) {
	m := readMarkers(renderPlist(residentJob()))
	if !m.Managed {
		t.Error("Managed should be true for a generated plist")
	}
	if m.Label != "keep.web" {
		t.Errorf("Label = %q, want keep.web", m.Label)
	}
	if m.Service != "web" {
		t.Errorf("Service = %q, want web", m.Service)
	}
	if m.KeepPath != "/Users/me/.local/bin/keep" {
		t.Errorf("KeepPath = %q", m.KeepPath)
	}
	if readMarkers([]byte("<plist><dict></dict></plist>")).Managed {
		t.Error("unmanaged plist must not be detected as managed")
	}
}

func TestMarkerExtractionUnescapesXML(t *testing.T) {
	j := residentJob()
	j.KeepPath = "/Users/me/A&B/keep"
	if got := readMarkers(renderPlist(j)).KeepPath; got != j.KeepPath {
		t.Errorf("KeepPath = %q, want %q", got, j.KeepPath)
	}
}

// A LaunchAgents directory holds more than plists; only this adapter's own
// file type can carry its markers.
func TestReadMarkersIgnoresNonPlistFiles(t *testing.T) {
	data := renderPlist(residentJob())
	r := &Runtime{artifactDir: "/agents"}
	if m := r.ReadMarkers("/agents/keep.web.plist", data); !m.Managed {
		t.Error("a generated plist should be managed")
	}
	if m := r.ReadMarkers("/agents/keep.web.json", data); m.Managed {
		t.Error("a non-plist file must never be reported as managed")
	}
}

func TestXMLEscaping(t *testing.T) {
	j := residentJob()
	j.ProgramArguments = []string{"/keep", "fork", "a&b<c>"}
	out := string(renderPlist(j))
	if !strings.Contains(out, "a&amp;b&lt;c&gt;") {
		t.Errorf("special chars not escaped:\n%s", out)
	}
}

// TestPlutilLint validates generated plists against the system plist linter.
func TestPlutilLint(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil not available")
	}
	jobs := map[string]runtime.Job{
		"resident": residentJob(),
		"interval": {Label: "keep.i", ProgramArguments: []string{"/keep", "fork", "i"}, StartInterval: 3600, Service: "i"},
		"calendar": {Label: "keep.c", ProgramArguments: []string{"/keep", "fork", "c"}, StartCalendar: []runtime.CalendarInterval{{Hour: intp(2), Minute: intp(30)}}, Service: "c"},
	}
	dir := t.TempDir()
	for name, j := range jobs {
		p := filepath.Join(dir, name+".plist")
		if err := os.WriteFile(p, renderPlist(j), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("plutil", "-lint", p).CombinedOutput()
		if err != nil {
			t.Errorf("plutil -lint %s failed: %v\n%s", name, err, out)
		}
	}
}
