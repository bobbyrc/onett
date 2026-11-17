package onboard

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ownerName matches "owner/name" as GitHub spells it. The dot is allowed, so "." and ".." need a
// separate check.
var ownerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// NormalizeRepo turns what a person typed into the form onett stores: "owner/name" for a GitHub
// repository, or a cleaned absolute path for a repository on this machine.
func NormalizeRepo(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		clean := filepath.Clean(s)
		if clean == "/" {
			return "", fmt.Errorf("%w: %q is the root of the disk, not a repository", ErrInvalid, s)
		}
		return clean, nil
	}
	rest := s
	for _, prefix := range []string{
		"https://github.com/", "https://www.github.com/", "git@github.com:",
	} {
		if after, ok := strings.CutPrefix(s, prefix); ok {
			rest = after
			break
		}
	}
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if ownerName.MatchString(rest) {
		owner, name, _ := strings.Cut(rest, "/")
		if owner != "." && owner != ".." && name != "." && name != ".." {
			return rest, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not owner/name, a GitHub link or an absolute path", ErrInvalid, s)
}

// IsGitHub reports whether a normalized repo is a GitHub repository rather than a path.
func IsGitHub(repo string) bool {
	return !strings.HasPrefix(repo, "/")
}

// NameFor returns the project name onett suggests for a repo: its last part, with characters the
// config does not allow replaced and leading punctuation dropped (see config.ProjectNamePattern).
func NameFor(repo string) string {
	base := repo[strings.LastIndex(repo, "/")+1:]
	base = strings.TrimSuffix(base, ".git")
	var b strings.Builder
	for _, r := range base {
		if isNameRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := strings.TrimLeftFunc(b.String(), func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	if name == "" {
		return "project"
	}
	return name
}

func isNameRune(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'
}

// suggestRepo returns the first repository in repos (the caller sorts them, most recently pushed
// first) whose name is close to requested's name: one contains the other, or they are at most two
// edits apart. It never returns requested itself, and "" when nothing is close.
func suggestRepo(requested string, repos []GitHubRepo) string {
	_, want, _ := strings.Cut(requested, "/")
	for _, r := range repos {
		if r.FullName == requested {
			continue
		}
		_, have, _ := strings.Cut(r.FullName, "/")
		if strings.HasPrefix(have, want) || strings.HasPrefix(want, have) || levenshtein(have, want) <= 2 {
			return r.FullName
		}
	}
	return ""
}

// levenshtein counts the single-character inserts, deletes and replacements that turn a into b.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
