// Package secrets reads and edits secrets.env, the KEY=value file that holds the credentials
// onett needs (bot tokens, API keys). The daemon loads it into its environment at startup, and
// the owner can edit it from the UI. Values are never logged or returned by the API.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// MaxValueBytes caps one value; real tokens are far smaller.
	MaxValueBytes = 8 << 10
	maxNameBytes  = 128
)

// ErrInvalid marks a name or value the file cannot hold.
var ErrInvalid = errors.New("invalid secret")

var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Pair is one KEY=value line.
type Pair struct {
	Key, Value string
}

// Parse reads KEY=value lines. Blank lines and lines starting with # are skipped, an optional
// leading "export " is dropped, and one pair of matching quotes around the value is removed.
// Lines without "=" or with an empty key are ignored.
func Parse(data []byte) []Pair {
	var out []Pair
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if k, v, ok := parseLine(sc.Text()); ok {
			out = append(out, Pair{Key: k, Value: v})
		}
	}
	return out
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if isQuoted(value) {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

func isQuoted(v string) bool {
	return len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'')
}

// Names returns the keys in the file, in file order without repeats. A missing file has none.
func Names(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	seen := map[string]bool{}
	names := []string{}
	for _, p := range Parse(data) {
		if !seen[p.Key] {
			seen[p.Key] = true
			names = append(names, p.Key)
		}
	}
	return names, nil
}

// Set writes NAME=value, replacing the existing line for NAME (and dropping repeats of it) or
// appending one. Other lines, comments included, are kept as they are.
func Set(path, name, value string) error {
	if err := validName(name); err != nil {
		return err
	}
	line, err := formatLine(name, value)
	if err != nil {
		return err
	}
	return rewrite(path, func(lines []string) []string {
		var out []string
		done := false
		for _, l := range lines {
			if k, _, ok := parseLine(l); ok && k == name {
				if !done {
					out = append(out, line)
					done = true
				}
				continue
			}
			out = append(out, l)
		}
		if !done {
			out = append(out, line)
		}
		return out
	})
}

// Delete removes every line for NAME. It reports whether the file had one.
func Delete(path, name string) (bool, error) {
	if err := validName(name); err != nil {
		return false, err
	}
	found := false
	err := rewrite(path, func(lines []string) []string {
		var out []string
		for _, l := range lines {
			if k, _, ok := parseLine(l); ok && k == name {
				found = true
				continue
			}
			out = append(out, l)
		}
		return out
	})
	return found, err
}

func validName(name string) error {
	if len(name) > maxNameBytes || !nameRE.MatchString(name) {
		return fmt.Errorf("%w: a name is letters, digits and _, and does not start with a digit", ErrInvalid)
	}
	return nil
}

// formatLine quotes the value only when the parser would otherwise change it.
func formatLine(name, value string) (string, error) {
	switch {
	case value == "":
		return "", fmt.Errorf("%w: the value is empty (remove the secret instead)", ErrInvalid)
	case len(value) > MaxValueBytes:
		return "", fmt.Errorf("%w: the value is longer than %d bytes", ErrInvalid, MaxValueBytes)
	case strings.ContainsAny(value, "\r\n\x00"):
		return "", fmt.Errorf("%w: the value has a line break", ErrInvalid)
	}
	if strings.TrimSpace(value) != value || isQuoted(value) {
		switch {
		case !strings.Contains(value, "'"):
			value = "'" + value + "'"
		case !strings.Contains(value, `"`):
			value = `"` + value + `"`
		default:
			return "", fmt.Errorf("%w: the value starts or ends with a space or quote and holds both quote kinds", ErrInvalid)
		}
	}
	return name + "=" + value, nil
}

// rewrite reads the file's lines, applies edit, and replaces the file atomically with mode 0600.
func rewrite(path string, edit func([]string) []string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read secrets: %w", err)
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
	lines = edit(lines)
	text := ""
	if len(lines) > 0 {
		text = strings.Join(lines, "\n") + "\n"
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*")
	if err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write secrets: %w", err)
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return fmt.Errorf("write secrets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	return nil
}
