package keep

import (
	"slices"
	"testing"
)

func TestSetEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{
			name: "appends when absent",
			env:  []string{"PATH=/usr/bin", "HOME=/Users/me"},
			want: []string{"PATH=/usr/bin", "HOME=/Users/me", "KEEP_SERVICE=opencode"},
		},
		{
			name: "replaces existing in place",
			env:  []string{"KEEP_SERVICE=stale", "PATH=/usr/bin"},
			want: []string{"KEEP_SERVICE=opencode", "PATH=/usr/bin"},
		},
		{
			name: "does not match prefix keys",
			env:  []string{"KEEP_SERVICE_EXTRA=x"},
			want: []string{"KEEP_SERVICE_EXTRA=x", "KEEP_SERVICE=opencode"},
		},
		{
			name: "empty env",
			env:  nil,
			want: []string{"KEEP_SERVICE=opencode"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setEnv(tt.env, "KEEP_SERVICE", "opencode")
			if !slices.Equal(got, tt.want) {
				t.Fatalf("setEnv(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
