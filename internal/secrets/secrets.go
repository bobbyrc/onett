// Package secrets owns secrets.env, the KEY=value file that holds credentials such as bot tokens
// and webhook URLs. The daemon loads it into its process environment before the config is read,
// because ${NAME} references and *_env options resolve from the environment. The same Store
// serves startup and the owner-only API, which can set and delete entries but never reads a
// value back. A value is never part of an error, a log line or a return value in this package.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound is returned by Delete when the name is not in the file.
var ErrNotFound = errors.New("secret not in the secrets file")

// ErrInvalid marks a name or value that cannot be stored.
var ErrInvalid = errors.New("invalid secret")

var nameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidName reports whether name is an environment variable name onett accepts as a secret.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Store is one secrets file and the record of which environment variables came from it.
type Store struct {
	path string

	mu sync.Mutex
	// fromFile holds the names this store put into the process environment. A variable set
	// before the store loaded it belongs to the real environment, which always wins.
	fromFile map[string]bool
}

// New returns a Store for the file at path. Nothing is read until Load.
func New(path string) *Store { return &Store{path: path, fromFile: map[string]bool{}} }

// Path is the secrets file's location.
func (s *Store) Path() string { return s.path }

// Load reads the file and applies it to the process environment. A missing file is not an error.
// Variables the real environment already set win, and so does the first line for a name that
// appears twice. Names this store set earlier and the file no longer holds are unset, so Load
// can run again after the file changed.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read secrets: %w", err)
	}
	entries := parse(data)
	inFile := map[string]bool{}
	for _, kv := range entries {
		if inFile[kv.key] {
			continue
		}
		inFile[kv.key] = true
		if !s.fromFile[kv.key] {
			if _, set := os.LookupEnv(kv.key); set {
				continue
			}
		}
		if err := os.Setenv(kv.key, kv.value); err != nil {
			return fmt.Errorf("set %s from secrets: %w", kv.key, err)
		}
		s.fromFile[kv.key] = true
	}
	for name := range s.fromFile {
		if !inFile[name] {
			_ = os.Unsetenv(name)
			delete(s.fromFile, name)
		}
	}
	return nil
}

// Names lists the names in the file, sorted, without values.
func (s *Store) Names() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.namesLocked()
}

func (s *Store) namesLocked() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	seen := map[string]bool{}
	var names []string
	for _, kv := range parse(data) {
		if !seen[kv.key] {
			seen[kv.key] = true
			names = append(names, kv.key)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Source says where name's value comes from: "file" when this store set it from the file,
// "environment" when the daemon's own environment holds it, "" when it is unset.
func (s *Store) Source(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.fromFile[name]:
		return "file"
	default:
		if _, set := os.LookupEnv(name); set {
			return "environment"
		}
		return ""
	}
}

// Set writes name=value to the file (atomically, mode 0600, other lines kept) and, unless the
// real environment owns the name, sets it in the process. shadowed reports that the real
// environment owns it, so the new value is saved but not in effect until that is removed.
func (s *Store) Set(name, value string) (shadowed bool, err error) {
	if !ValidName(name) {
		return false, fmt.Errorf("%w: name must match %s", ErrInvalid, nameRe)
	}
	if value == "" {
		return false, fmt.Errorf("%w: value must not be empty", ErrInvalid)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return false, fmt.Errorf("%w: value must be a single line", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read secrets: %w", err)
	}
	if err := s.writeLocked(rewrite(data, name, formatLine(name, value))); err != nil {
		return false, err
	}
	if !s.fromFile[name] {
		if _, set := os.LookupEnv(name); set {
			return true, nil
		}
	}
	if err := os.Setenv(name, value); err != nil {
		return false, fmt.Errorf("set %s: %w", name, err)
	}
	s.fromFile[name] = true
	return false, nil
}

// Delete removes name from the file and, when this store had set it, from the process.
// ErrNotFound means the file does not hold it.
func (s *Store) Delete(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: name must match %s", ErrInvalid, nameRe)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read secrets: %w", err)
	}
	found := false
	for _, kv := range parse(data) {
		found = found || kv.key == name
	}
	if !found {
		return ErrNotFound
	}
	if err := s.writeLocked(rewrite(data, name, "")); err != nil {
		return err
	}
	if s.fromFile[name] {
		_ = os.Unsetenv(name)
		delete(s.fromFile, name)
	}
	return nil
}

// writeLocked replaces the file with data: temp file in the same directory, then rename. The
// directory is created 0700 and the file is always 0600. A symlinked file is written through.
func (s *Store) writeLocked(data []byte) error {
	target := s.path
	if resolved, err := filepath.EvalSymlinks(s.path); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp secrets file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Chmod(0o600)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write secrets: %w", werr)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace secrets file: %w", err)
	}
	return nil
}

// formatLine renders one line. The parser drops one pair of matching quotes and trims space,
// so a value that those rules would change is wrapped in double quotes to survive.
func formatLine(name, value string) string {
	if value != strings.TrimSpace(value) || (len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0]) {
		value = `"` + value + `"`
	}
	return name + "=" + value
}

// rewrite returns data with the first line for name replaced by line and later lines for name
// dropped. An empty line removes every line for name. A new name is appended.
func rewrite(data []byte, name, line string) []byte {
	var out []string
	done := false
	for _, raw := range splitLines(data) {
		if key, _, ok := parseLine(raw); ok && key == name {
			if line != "" && !done {
				out = append(out, line)
				done = true
			}
			continue
		}
		out = append(out, raw)
	}
	if line != "" && !done {
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

func splitLines(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

type entry struct{ key, value string }

// parse reads KEY=value lines. Blank lines and lines starting with # are skipped, an optional
// leading "export " is dropped, and one pair of matching quotes around the value is removed.
// Lines without "=" or with an empty key are ignored.
func parse(data []byte) []entry {
	var out []entry
	for _, raw := range splitLines(data) {
		if key, value, ok := parseLine(raw); ok {
			out = append(out, entry{key, value})
		}
	}
	return out
}

func parseLine(raw string) (key, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, value, ok = strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}
