package keep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":     0,
		"100":  100,
		"100B": 100,
		"10KB": 10 << 10,
		"10K":  10 << 10,
		"5MB":  5 << 20,
		"1GB":  1 << 30,
	}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil {
			t.Errorf("parseSize(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestRotateCopytruncateReclaimsSpace verifies the chosen mechanism: launchd
// holds the log handle open in append mode, so truncating in place reclaims
// space and subsequent appends do not create a sparse hole (issue #8).
func TestRotateCopytruncateReclaimsSpace(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "svc.out.log")

	// Simulate launchd: an open file handle in O_APPEND mode.
	handle, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	big := make([]byte, 4096)
	for i := range big {
		big[i] = 'x'
	}
	if _, err := handle.Write(big); err != nil {
		t.Fatal(err)
	}

	rotated, err := rotateFile(logPath, 1024, 0, 1)
	if err != nil {
		t.Fatalf("rotateFile: %v", err)
	}
	if !rotated {
		t.Fatal("expected rotation to occur")
	}

	// Original truncated to 0; the rotated copy holds the old content.
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Errorf("after copytruncate, live log size = %d, want 0", fi.Size())
	}
	rotatedFi, err := os.Stat(logPath + ".1")
	if err != nil {
		t.Fatalf("rotated copy missing: %v", err)
	}
	if rotatedFi.Size() != 4096 {
		t.Errorf("rotated copy size = %d, want 4096", rotatedFi.Size())
	}

	// The still-open handle appends at the new EOF — no sparse hole.
	if _, err := handle.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(logPath)
	if fi.Size() != int64(len("after\n")) {
		t.Errorf("post-rotation append produced size %d, want %d (space reclaimed, no sparse hole)", fi.Size(), len("after\n"))
	}
}

func TestRotateSkipsWhenUnderThreshold(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "svc.out.log")
	if err := os.WriteFile(logPath, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	rotated, err := rotateFile(logPath, 1<<20, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Error("should not rotate a file under the size threshold")
	}
}
