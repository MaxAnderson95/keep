package launchd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func intp(i int) *int { return &i }

func residentJob() Job {
	return Job{
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
	a := Render(residentJob())
	b := Render(residentJob())
	if string(a) != string(b) {
		t.Fatal("Render is not deterministic")
	}
}

func TestRenderResidentContents(t *testing.T) {
	out := string(Render(residentJob()))
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
	j := Job{
		Label:            "keep.backup",
		ProgramArguments: []string{"/keep", "fork", "backup"},
		StartInterval:    21600,
		Service:          "backup",
	}
	out := string(Render(j))
	if !strings.Contains(out, "<key>StartInterval</key>") || !strings.Contains(out, "<integer>21600</integer>") {
		t.Errorf("missing StartInterval:\n%s", out)
	}
	if strings.Contains(out, "RunAtLoad") || strings.Contains(out, "KeepAlive") {
		t.Error("scheduled job must not have RunAtLoad/KeepAlive")
	}
}

func TestRenderScheduledCalendar(t *testing.T) {
	j := Job{
		Label:            "keep.backup",
		ProgramArguments: []string{"/keep", "fork", "backup"},
		StartCalendar: []CalendarInterval{
			{Hour: intp(2), Minute: intp(30)},
			{Hour: intp(14), Minute: intp(0)},
		},
		Service: "backup",
	}
	out := string(Render(j))
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
	data := Render(residentJob())
	if !IsManaged(data) {
		t.Error("IsManaged should be true for generated plist")
	}
	if got := MarkerService(data); got != "web" {
		t.Errorf("MarkerService = %q, want web", got)
	}
	if got := MarkerKeepPath(data); got != "/Users/me/.local/bin/keep" {
		t.Errorf("MarkerKeepPath = %q", got)
	}
	if IsManaged([]byte("<plist><dict></dict></plist>")) {
		t.Error("unmanaged plist must not be detected as managed")
	}
}

func TestMarkerExtractionUnescapesXML(t *testing.T) {
	j := residentJob()
	j.KeepPath = "/Users/me/A&B/keep"
	data := Render(j)
	if got := MarkerKeepPath(data); got != j.KeepPath {
		t.Errorf("MarkerKeepPath = %q, want %q", got, j.KeepPath)
	}
}

func TestXMLEscaping(t *testing.T) {
	j := residentJob()
	j.ProgramArguments = []string{"/keep", "fork", "a&b<c>"}
	out := string(Render(j))
	if !strings.Contains(out, "a&amp;b&lt;c&gt;") {
		t.Errorf("special chars not escaped:\n%s", out)
	}
}

// TestPlutilLint validates generated plists against the system plist linter.
func TestPlutilLint(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil not available")
	}
	jobs := map[string]Job{
		"resident": residentJob(),
		"interval": {Label: "keep.i", ProgramArguments: []string{"/keep", "fork", "i"}, StartInterval: 3600, Service: "i"},
		"calendar": {Label: "keep.c", ProgramArguments: []string{"/keep", "fork", "c"}, StartCalendar: []CalendarInterval{{Hour: intp(2), Minute: intp(30)}}, Service: "c"},
	}
	dir := t.TempDir()
	for name, j := range jobs {
		p := filepath.Join(dir, name+".plist")
		if err := os.WriteFile(p, Render(j), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("plutil", "-lint", p).CombinedOutput()
		if err != nil {
			t.Errorf("plutil -lint %s failed: %v\n%s", name, err, out)
		}
	}
}
