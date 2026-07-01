package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// DotenvVar is one parsed assignment, in file order.
type DotenvVar struct {
	Key   string
	Value string
}

// ParseDotenv parses dotenv-format bytes: KEY=value / export KEY=value, with
// comments and quotes. It is a plain-assignment parser — no shell logic, no
// interpolation (D7, D22).
func ParseDotenv(data []byte) ([]DotenvVar, error) {
	var out []DotenvVar
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: not a KEY=value assignment: %q", lineNo, line)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		val := parseDotenvValue(strings.TrimSpace(line[eq+1:]))
		out = append(out, DotenvVar{Key: key, Value: val})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseDotenvValue(raw string) string {
	if raw == "" {
		return ""
	}
	switch raw[0] {
	case '\'':
		// Single quotes: literal, no inline comment handling inside.
		if end := strings.IndexByte(raw[1:], '\''); end >= 0 {
			return raw[1 : 1+end]
		}
		return raw[1:]
	case '"':
		// Double quotes: strip surrounding quotes, unescape \n \t \" \\.
		if end := closingDoubleQuote(raw); end >= 0 {
			return unescapeDouble(raw[1:end])
		}
		return unescapeDouble(raw[1:])
	default:
		// Unquoted: an inline comment starts at the first " #".
		if idx := strings.Index(raw, " #"); idx >= 0 {
			raw = raw[:idx]
		}
		return strings.TrimSpace(raw)
	}
}

func closingDoubleQuote(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}

func unescapeDouble(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// LoadDotenvFile reads and parses a dotenv file from disk.
func LoadDotenvFile(path string) ([]DotenvVar, error) {
	data, err := os.ReadFile(ExpandPath(path))
	if err != nil {
		return nil, err
	}
	vars, err := ParseDotenv(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return vars, nil
}
