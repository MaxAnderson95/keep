package config

import "testing"

func TestParseDotenv(t *testing.T) {
	data := `
# a comment
KEY=value
export EXPORTED=yes
QUOTED="hello world"
SINGLE='literal $value'
WITH_COMMENT=foo # trailing comment
ESCAPED="line1\nline2"
EMPTY=
SPACED = trimmed
`
	vars, err := ParseDotenv([]byte(data))
	if err != nil {
		t.Fatalf("ParseDotenv: %v", err)
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v.Key] = v.Value
	}
	want := map[string]string{
		"KEY":          "value",
		"EXPORTED":     "yes",
		"QUOTED":       "hello world",
		"SINGLE":       "literal $value",
		"WITH_COMMENT": "foo",
		"ESCAPED":      "line1\nline2",
		"EMPTY":        "",
		"SPACED":       "trimmed",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestParseDotenvOrderPreserved(t *testing.T) {
	vars, err := ParseDotenv([]byte("A=1\nB=2\nC=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"A", "B", "C"}
	for i, v := range vars {
		if v.Key != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, v.Key, want[i])
		}
	}
}

func TestParseDotenvBadLine(t *testing.T) {
	if _, err := ParseDotenv([]byte("not_an_assignment\n")); err == nil {
		t.Error("expected error for non-assignment line")
	}
}
