package config

import (
	"fmt"
	"regexp"
	"strings"
)

var serviceNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Validate checks the whole Config for structural errors. It never touches
// launchd — it is a pure check of the declarative source.
func (c *Config) Validate() error {
	if len(c.Services) == 0 {
		return fmt.Errorf("config defines no services")
	}
	labels := map[string]string{}
	for i := range c.Services {
		s := &c.Services[i]
		if err := validateService(s); err != nil {
			return err
		}
		label := s.EffectiveLabel()
		if other, ok := labels[label]; ok {
			return fmt.Errorf("service %q and service %q resolve to the same label %q", s.Name, other, label)
		}
		labels[label] = s.Name
	}
	return nil
}

func validateService(s *Service) error {
	if !serviceNameRE.MatchString(s.Name) {
		return fmt.Errorf("service %q: name must match %s", s.Name, serviceNameRE.String())
	}

	switch s.Type {
	case TypeResident, TypeScheduled:
	default:
		return fmt.Errorf("service %q: type must be %q or %q (got %q)", s.Name, TypeResident, TypeScheduled, s.Type)
	}

	// command xor args (D16).
	hasCommand := strings.TrimSpace(s.Command) != ""
	hasArgs := len(s.Args) > 0
	switch {
	case hasCommand && hasArgs:
		return fmt.Errorf("service %q: set exactly one of 'command' or 'args', not both", s.Name)
	case !hasCommand && !hasArgs:
		return fmt.Errorf("service %q: set exactly one of 'command' or 'args'", s.Name)
	}
	if hasCommand {
		words, err := SplitCommand(s.Command)
		if err != nil {
			return fmt.Errorf("service %q: command: %w", s.Name, err)
		}
		if len(words) == 0 {
			return fmt.Errorf("service %q: command is empty after splitting", s.Name)
		}
	}

	// type / schedule consistency (D17, D18).
	if s.IsScheduled() {
		if s.Schedule == nil {
			return fmt.Errorf("service %q: type 'scheduled' requires a 'schedule' block", s.Name)
		}
		if err := validateSchedule(s); err != nil {
			return err
		}
	} else if s.Schedule != nil {
		return fmt.Errorf("service %q: 'schedule' is only valid on a scheduled service", s.Name)
	}

	if s.Umask != "" {
		if _, err := parseUmask(s.Umask); err != nil {
			return fmt.Errorf("service %q: umask: %w", s.Name, err)
		}
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("service %q: port %d out of range", s.Name, s.Port)
	}
	if err := validateUpdate(s); err != nil {
		return err
	}
	return nil
}

// validateUpdate checks the update command list and timeout (U1, U7).
func validateUpdate(s *Service) error {
	for i, cmd := range s.Update {
		words, err := SplitCommand(cmd)
		if err != nil {
			return fmt.Errorf("service %q: update[%d]: %w", s.Name, i, err)
		}
		if len(words) == 0 {
			return fmt.Errorf("service %q: update[%d] is empty", s.Name, i)
		}
	}
	if strings.TrimSpace(s.UpdateTimeout) != "" {
		if !s.HasUpdate() {
			return fmt.Errorf("service %q: update_timeout is only valid with update commands", s.Name)
		}
		if _, err := s.UpdateTimeoutDuration(); err != nil {
			return fmt.Errorf("service %q: update_timeout: %w", s.Name, err)
		}
	}
	return nil
}

func validateSchedule(s *Service) error {
	sc := s.Schedule
	hasCal := len(sc.Calendar) > 0
	hasInterval := strings.TrimSpace(sc.Interval) != ""
	switch {
	case hasCal && hasInterval:
		return fmt.Errorf("service %q: schedule must set exactly one of 'calendar' or 'interval', not both", s.Name)
	case !hasCal && !hasInterval:
		return fmt.Errorf("service %q: schedule must set exactly one of 'calendar' or 'interval'", s.Name)
	}
	if hasInterval {
		secs, err := ParseInterval(sc.Interval)
		if err != nil {
			return fmt.Errorf("service %q: schedule.interval: %w", s.Name, err)
		}
		if secs <= 0 {
			return fmt.Errorf("service %q: schedule.interval must be positive", s.Name)
		}
	}
	for _, ci := range sc.Calendar {
		if err := validateCalendar(s.Name, ci); err != nil {
			return err
		}
	}
	return nil
}

func validateCalendar(svc string, ci CalendarInterval) error {
	check := func(name string, v *int, lo, hi int) error {
		if v == nil {
			return nil
		}
		if *v < lo || *v > hi {
			return fmt.Errorf("service %q: schedule.calendar.%s=%d out of range [%d,%d]", svc, name, *v, lo, hi)
		}
		return nil
	}
	if ci.Minute == nil && ci.Hour == nil && ci.Day == nil && ci.Weekday == nil && ci.Month == nil {
		return fmt.Errorf("service %q: schedule.calendar entry must set at least one field", svc)
	}
	if err := check("minute", ci.Minute, 0, 59); err != nil {
		return err
	}
	if err := check("hour", ci.Hour, 0, 23); err != nil {
		return err
	}
	if err := check("day", ci.Day, 1, 31); err != nil {
		return err
	}
	if err := check("weekday", ci.Weekday, 0, 7); err != nil {
		return err
	}
	if err := check("month", ci.Month, 1, 12); err != nil {
		return err
	}
	return nil
}
