package config

import (
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`/bin/echo hello world`, []string{"/bin/echo", "hello", "world"}},
		{`/bin/echo "hello world"`, []string{"/bin/echo", "hello world"}},
		{`/bin/echo 'a b' c`, []string{"/bin/echo", "a b", "c"}},
		{`cmd --flag="a b"`, []string{"cmd", "--flag=a b"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`opencode serve --hostname 127.0.0.1 --port 4096`, []string{"opencode", "serve", "--hostname", "127.0.0.1", "--port", "4096"}},
		{`  spaced   out  `, []string{"spaced", "out"}},
	}
	for _, tc := range cases {
		got, err := SplitCommand(tc.in)
		if err != nil {
			t.Errorf("SplitCommand(%q) error: %v", tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitCommand(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestSplitCommandErrors(t *testing.T) {
	for _, in := range []string{`"unterminated`, `bad\`, `'open`} {
		if _, err := SplitCommand(in); err == nil {
			t.Errorf("SplitCommand(%q) expected error", in)
		}
	}
}

func TestParseInterval(t *testing.T) {
	cases := map[string]int{
		"6h":    6 * 3600,
		"30m":   30 * 60,
		"90s":   90,
		"1d":    24 * 3600,
		"2d":    2 * 24 * 3600,
		"1h30m": 5400,
	}
	for in, want := range cases {
		got, err := ParseInterval(in)
		if err != nil {
			t.Errorf("ParseInterval(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseInterval(%q) = %d, want %d", in, got, want)
		}
	}
	if _, err := ParseInterval("nonsense"); err == nil {
		t.Error("expected error for nonsense interval")
	}
}
