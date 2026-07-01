package cli

import (
	"reflect"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			"flag after positional",
			[]string{"keep", "logs", "web", "-n", "5"},
			[]string{"keep", "logs", "-n", "5", "web"},
		},
		{
			"follow after positional",
			[]string{"keep", "logs", "web", "-f"},
			[]string{"keep", "logs", "-f", "web"},
		},
		{
			"json after positional",
			[]string{"keep", "status", "web", "--json"},
			[]string{"keep", "status", "--json", "web"},
		},
		{
			"already ordered",
			[]string{"keep", "logs", "-n", "5", "web"},
			[]string{"keep", "logs", "-n", "5", "web"},
		},
		{
			"global flag before command preserved",
			[]string{"keep", "--config", "/x.yaml", "status", "web", "--json"},
			[]string{"keep", "--config", "/x.yaml", "status", "--json", "web"},
		},
		{
			"multiple positionals",
			[]string{"keep", "up", "a", "b", "c"},
			[]string{"keep", "up", "a", "b", "c"},
		},
		{
			"double dash passthrough",
			[]string{"keep", "logs", "web", "--", "-f"},
			[]string{"keep", "logs", "web", "--", "-f"},
		},
		{
			"no command",
			[]string{"keep"},
			[]string{"keep"},
		},
		{
			"equals form value flag",
			[]string{"keep", "logs", "web", "--lines=10"},
			[]string{"keep", "logs", "--lines=10", "web"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reorderArgs(newApp(BuildInfo{Version: "test"}), tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
