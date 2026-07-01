package config

import (
	"strings"
	"testing"
)

func parse(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

func TestParseDefaultsTypeResident(t *testing.T) {
	cfg := parse(t, `
services:
  web:
    command: /usr/bin/true
`)
	if len(cfg.Services) != 1 {
		t.Fatalf("want 1 service, got %d", len(cfg.Services))
	}
	s := cfg.Services[0]
	if s.Type != TypeResident {
		t.Errorf("type default = %q, want %q", s.Type, TypeResident)
	}
	if s.EffectiveLabel() != "keep.web" {
		t.Errorf("label = %q, want keep.web", s.EffectiveLabel())
	}
	if !s.IsEnabled() {
		t.Error("service should be enabled by default")
	}
}

func TestValidateCommandXorArgs(t *testing.T) {
	cases := map[string]string{
		"both": `
services:
  x:
    command: /bin/true
    args: ["/bin/true"]
`,
		"neither": `
services:
  x:
    type: resident
`,
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := parse(t, y)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateArgsOnly(t *testing.T) {
	cfg := parse(t, `
services:
  x:
    args: ["/usr/bin/env", "-i"]
`)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnabledFalse(t *testing.T) {
	cfg := parse(t, `
services:
  x:
    command: /bin/true
    enabled: false
`)
	if cfg.Services[0].IsEnabled() {
		t.Error("enabled:false should be declared off")
	}
}

func TestScheduledRequiresSchedule(t *testing.T) {
	cfg := parse(t, `
services:
  job:
    type: scheduled
    command: /bin/true
`)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires a 'schedule'") {
		t.Fatalf("want schedule-required error, got %v", err)
	}
}

func TestScheduleOnResidentErrors(t *testing.T) {
	cfg := parse(t, `
services:
  x:
    command: /bin/true
    schedule:
      interval: 6h
`)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "only valid on a scheduled") {
		t.Fatalf("want schedule-on-resident error, got %v", err)
	}
}

func TestScheduleExactlyOne(t *testing.T) {
	cfg := parse(t, `
services:
  job:
    type: scheduled
    command: /bin/true
    schedule:
      interval: 6h
      calendar: { hour: 2 }
`)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("want exactly-one error, got %v", err)
	}
}

func TestCalendarSingleAndList(t *testing.T) {
	single := parse(t, `
services:
  job:
    type: scheduled
    command: /bin/true
    schedule:
      calendar: { hour: 2, minute: 30 }
`)
	if err := single.Validate(); err != nil {
		t.Fatalf("single calendar: %v", err)
	}
	if got := len(single.Services[0].Schedule.Calendar); got != 1 {
		t.Fatalf("single calendar entries = %d, want 1", got)
	}

	list := parse(t, `
services:
  job:
    type: scheduled
    command: /bin/true
    schedule:
      calendar:
        - { hour: 2, minute: 30 }
        - { hour: 14, minute: 0 }
`)
	if err := list.Validate(); err != nil {
		t.Fatalf("list calendar: %v", err)
	}
	if got := len(list.Services[0].Schedule.Calendar); got != 2 {
		t.Fatalf("list calendar entries = %d, want 2", got)
	}
}

func TestCalendarRangeValidation(t *testing.T) {
	cfg := parse(t, `
services:
  job:
    type: scheduled
    command: /bin/true
    schedule:
      calendar: { hour: 30 }
`)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want range error, got %v", err)
	}
}

func TestDuplicateLabelErrors(t *testing.T) {
	cfg := parse(t, `
services:
  a:
    command: /bin/true
    label: shared
  b:
    command: /bin/true
    label: shared
`)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "same label") {
		t.Fatalf("want duplicate-label error, got %v", err)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte(`
services:
  x:
    command: /bin/true
    bogus_field: true
`))
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
}
