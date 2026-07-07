package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.out.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTailFileSmallFile(t *testing.T) {
	path := writeLog(t, "one\ntwo\nthree\n")
	lines, size, err := tailFile(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("one\ntwo\nthree\n")) {
		t.Fatalf("size = %d", size)
	}
	if len(lines) != 2 || lines[0] != "two" || lines[1] != "three" {
		t.Fatalf("lines = %v, want [two three]", lines)
	}
}

func TestTailFileWindowBoundsRead(t *testing.T) {
	// 1000 numbered lines, then tail with a window far smaller than the file:
	// the result must come only from the end, with the partial first line of
	// the window dropped rather than returned truncated.
	var b strings.Builder
	for range 1000 {
		b.WriteString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n") // 41 bytes
	}
	b.WriteString("last-line\n")
	path := writeLog(t, b.String())

	lines, size, err := tailFileWindow(path, 5, 100)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(b.Len()) {
		t.Fatalf("size = %d, want %d (must reflect the full file for the follower offset)", size, b.Len())
	}
	if len(lines) == 0 || lines[len(lines)-1] != "last-line" {
		t.Fatalf("lines = %v, want to end with last-line", lines)
	}
	for _, l := range lines {
		if l != "last-line" && l != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Fatalf("got a truncated partial line %q — window boundary line must be dropped", l)
		}
	}
}

func TestTailFileWindowNoNewlineInWindow(t *testing.T) {
	// One giant line larger than the window: nothing complete to return.
	path := writeLog(t, strings.Repeat("x", 500))
	lines, size, err := tailFileWindow(path, 5, 100)
	if err != nil {
		t.Fatal(err)
	}
	if size != 500 {
		t.Fatalf("size = %d, want 500", size)
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %v, want none (window is mid-line with no newline)", lines)
	}
}

func TestTailFileEmpty(t *testing.T) {
	path := writeLog(t, "")
	lines, size, err := tailFile(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if size != 0 || len(lines) != 0 {
		t.Fatalf("lines=%v size=%d, want none/0", lines, size)
	}
}
