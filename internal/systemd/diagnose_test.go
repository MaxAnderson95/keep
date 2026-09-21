package systemd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/runtime"
)

func diagnoser(s *scriptedSystemctl, linger string, lingerErr error) *Runtime {
	r := s.adapter()
	r.loginctl = func(_ context.Context, _ ...string) (string, error) { return linger, lingerErr }
	return r
}

func problems(ds []runtime.Diagnosis) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(string(d.Severity) + ": " + d.Problem + "\n")
	}
	return b.String()
}

func TestDiagnoseCleanEnvironmentIsSilent(t *testing.T) {
	s := newScripted()
	s.version = "systemd 255 (255.4-1ubuntu8.4)"
	if got := diagnoser(s, "Linger=yes\n", nil).Diagnose(); len(got) != 0 {
		t.Errorf("expected no findings, got:\n%s", problems(got))
	}
}

// Without lingering the user manager dies at logout, which silently stops
// every Service keep just started.
func TestDiagnoseWarnsWhenLingeringIsOff(t *testing.T) {
	s := newScripted()
	s.version = "systemd 255 (255.4-1ubuntu8.4)"
	got := diagnoser(s, "Linger=no\n", nil).Diagnose()
	if len(got) != 1 || got[0].Severity != runtime.SevWarning || !strings.Contains(got[0].Problem, "lingering is off") {
		t.Fatalf("want one lingering warning, got:\n%s", problems(got))
	}
	if !strings.Contains(got[0].Fix, "enable-linger") {
		t.Errorf("the fix should name the command: %q", got[0].Fix)
	}
}

func TestDiagnoseRejectsSystemdOlderThanAppend(t *testing.T) {
	s := newScripted()
	s.version = "systemd 239 (239-78.el8)"
	got := diagnoser(s, "Linger=yes\n", nil).Diagnose()
	if len(got) != 1 || got[0].Severity != runtime.SevError {
		t.Fatalf("want one version error, got:\n%s", problems(got))
	}
	if !strings.Contains(got[0].Problem, "239") || !strings.Contains(got[0].Problem, "240") {
		t.Errorf("the problem should name both versions: %q", got[0].Problem)
	}
}

// Everything else asks the same unreachable manager, so reporting it once is
// the whole diagnosis.
func TestDiagnoseUnreachableManagerIsTheOnlyFinding(t *testing.T) {
	s := newScripted()
	s.version = "systemd 239 (239-78.el8)"
	s.fail["show-environment"] = "Failed to connect to bus: No medium found"
	got := diagnoser(s, "Linger=no\n", nil).Diagnose()
	if len(got) != 1 {
		t.Fatalf("want exactly one finding, got:\n%s", problems(got))
	}
	if got[0].Severity != runtime.SevError || !strings.Contains(got[0].Problem, "not reachable") {
		t.Errorf("finding = %+v", got[0])
	}
}

// doctor never invents a problem it cannot support: an unreadable linger state
// is not evidence that lingering is off.
func TestDiagnoseStaysQuietWhenLingerIsUnreadable(t *testing.T) {
	s := newScripted()
	s.version = "systemd 255 (255.4-1ubuntu8.4)"
	if got := diagnoser(s, "", errors.New("loginctl: not found")).Diagnose(); len(got) != 0 {
		t.Errorf("expected no findings, got:\n%s", problems(got))
	}
}

// An unreadable version is not evidence of an old one either.
func TestDiagnoseStaysQuietWhenVersionIsUnreadable(t *testing.T) {
	s := newScripted()
	s.version = "some other tool entirely"
	if got := diagnoser(s, "Linger=yes\n", nil).Diagnose(); len(got) != 0 {
		t.Errorf("expected no findings, got:\n%s", problems(got))
	}
}
