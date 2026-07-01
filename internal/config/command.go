package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SplitCommand splits a command string into argv the way a POSIX shell would
// tokenize words — honoring single quotes, double quotes, and backslash
// escapes — but WITHOUT spawning a shell or performing any expansion (D16).
func SplitCommand(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inWord  bool
		inS     bool // inside single quotes
		inD     bool // inside double quotes
		escaped bool
	)
	flush := func() {
		if inWord {
			args = append(args, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			inWord = true
			escaped = false
		case inS:
			if c == '\'' {
				inS = false
			} else {
				cur.WriteByte(c)
			}
		case inD:
			if c == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\' || s[i+1] == '$' || s[i+1] == '`') {
				escaped = true
			} else if c == '"' {
				inD = false
			} else {
				cur.WriteByte(c)
			}
		default:
			switch c {
			case '\\':
				escaped = true
				inWord = true
			case '\'':
				inS = true
				inWord = true
			case '"':
				inD = true
				inWord = true
			case ' ', '\t', '\n', '\r':
				flush()
			default:
				cur.WriteByte(c)
				inWord = true
			}
		}
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if inS || inD {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return args, nil
}

// ResolveArgv returns the explicit argv for a Service from either Args or the
// shell-word-split Command. The first token has a leading ~ expanded.
func (s *Service) ResolveArgv() ([]string, error) {
	var argv []string
	if len(s.Args) > 0 {
		argv = append(argv, s.Args...)
	} else {
		words, err := SplitCommand(s.Command)
		if err != nil {
			return nil, err
		}
		argv = words
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("service %q has no command", s.Name)
	}
	argv[0] = ExpandPath(argv[0])
	return argv, nil
}

// ParsedUmask returns the Service's umask as an integer. ok is false when no
// umask was declared.
func (s *Service) ParsedUmask() (mask int, ok bool, err error) {
	if s.Umask == "" {
		return 0, false, nil
	}
	v, err := parseUmask(s.Umask)
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

func parseUmask(s string) (int, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 8, 32)
	if err != nil {
		return 0, fmt.Errorf("must be an octal mask like \"077\": %w", err)
	}
	if v < 0 || v > 0o777 {
		return 0, fmt.Errorf("out of range")
	}
	return int(v), nil
}

// ParseInterval parses a schedule interval into whole seconds. It accepts Go
// durations ("6h", "30m", "90s") plus a bare-day suffix ("1d").
func ParseInterval(s string) (int, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return n * 24 * 3600, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return int(d.Seconds()), nil
}
