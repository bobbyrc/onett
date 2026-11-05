package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ProjectNamePattern matches names usable as YAML keys and in CLI flags.
var ProjectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// SelfRepo is onett's own repository; a project for it becomes self_project.
const SelfRepo = "bobbyrc/onett"

// NewProject is what AddProject writes for a new project. Only Name and Repo are required; an
// empty field is left out of the file, so the project keeps the default for it.
type NewProject struct {
	Name, Repo   string
	Path, Branch string // Branch is written as default_branch
	Policy       string
	Instructions string
}

// ErrProjectExists is matched (errors.Is) by the error AddProject returns for a duplicate name.
var ErrProjectExists = errors.New("project already exists")

type existsError struct{ msg string }

func (e *existsError) Error() string        { return e.msg }
func (e *existsError) Is(target error) bool { return target == ErrProjectExists }

// InvalidResultError means the file with the project added would not load. Err is the load error,
// which ProblemsFrom can split into one problem per setting.
type InvalidResultError struct {
	Name, Path string
	Err        error
}

func (e *InvalidResultError) Error() string {
	return fmt.Sprintf("adding project %q would make %s invalid, so it was not changed: %v", e.Name, e.Path, e.Err)
}
func (e *InvalidResultError) Unwrap() error { return e.Err }

// AddProject returns the config text with the project p added. It is the one implementation behind
// `onett project add` and POST /projects. The YAML node tree is used only
// to find where the new lines go. The text is never re-encoded: re-encoding would reflow the
// author's comments and blank lines, so the result is the original bytes with one block of lines
// inserted. The result must still load as a valid config; otherwise an error is returned and the
// caller writes nothing.
func AddProject(data []byte, path string, p NewProject) ([]byte, error) {
	name := p.Name
	if !ProjectNamePattern.MatchString(name) {
		return nil, fmt.Errorf("project name %q may use letters, digits, '.', '_' and '-'", name)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// A file with no mapping at all (empty, or only comments) has no root and gets the new keys
	// appended.
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	var projKey, projects, selfProject *yaml.Node
	var setsSelf bool
	projIdx := -1
	if root != nil {
		if root.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: top level is not a mapping", path)
		}
		if root.Style&yaml.FlowStyle != 0 {
			return nil, errFlowStyle(path)
		}
		for i := 0; i+1 < len(root.Content); i += 2 {
			switch root.Content[i].Value {
			case "projects":
				projIdx, projKey, projects = i, root.Content[i], root.Content[i+1]
			case "self_project":
				selfProject = root.Content[i+1]
			}
		}
	}

	lines := splitLines(string(data))
	var prefix []string
	var at int
	var indent string
	switch {
	case projects == nil:
		// No projects key yet: it is appended at the end of the file, after a blank line.
		at = len(lines)
		if len(lines) > 0 {
			prefix = append(prefix, "\n")
		}
		prefix = append(prefix, "projects:\n")
		indent = "  "
	case projects.ShortTag() == "!!null" && projects.Value == "":
		// An empty "projects:" line: the entry goes on the line right after it.
		at = projKey.Line
		indent = "  "
	case projects.ShortTag() == "!!null":
		return nil, errFlowStyle(path)
	case projects.Kind != yaml.MappingNode:
		return nil, fmt.Errorf("%s: projects is not a mapping", path)
	case projects.Style&yaml.FlowStyle != 0 || len(projects.Content) == 0:
		return nil, errFlowStyle(path)
	default:
		if mappingValue(projects, name) != nil {
			return nil, &existsError{fmt.Sprintf("project %q already exists in %s", name, path)}
		}
		// The entry is indented to match the projects already there.
		indent = strings.Repeat(" ", projects.Content[0].Column-1)
		at = projectsEnd(lines, root, projIdx, projKey.Line-1, len(indent))
	}

	entry, err := entryLines(indent, p)
	if err != nil {
		return nil, err
	}
	lines = insertLines(lines, at, append(prefix, entry...))

	// Onett's own repository becomes the self_project, as init does, but an existing choice
	// is never overwritten.
	if p.Repo == SelfRepo && selfProject == nil {
		setsSelf = true
		lines = insertLines(lines, len(lines), []string{"self_project: " + name + "\n"})
	}

	out := []byte(strings.Join(lines, ""))
	if _, err := Parse(out, path); err != nil {
		return nil, &InvalidResultError{Name: name, Path: path, Err: err}
	}
	if err := CheckInsertion(data, out, path, name, setsSelf); err != nil {
		return nil, err
	}
	// The new project must read back as given; a text that block style mangled is refused
	// rather than saved altered.
	if got, _ := Parse(out, path); got == nil || got.Projects[name] == nil ||
		got.Projects[name].Instructions != p.Instructions || got.Projects[name].Repo != p.Repo {
		return nil, fmt.Errorf("the new project %q would not read back as given, so %s was not changed", name, path)
	}
	return out, nil
}

// CheckInsertion is the semantic guard for insertProject. The text can still load and yet hold
// different settings: a block scalar cut short by an inserted line, for example, parses without
// error. So both versions are parsed, the added project and (when setsSelf is true) the new
// self_project are removed from the new one, and the two must then be equal. Any other difference
// is an error, and the caller writes nothing.
func CheckInsertion(before, after []byte, path, name string, setsSelf bool) error {
	oldCfg, err := Parse(before, path)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	newCfg, err := Parse(after, path)
	if err != nil {
		return fmt.Errorf("parse new %s: %w", path, err)
	}
	delete(newCfg.Projects, name)
	if setsSelf {
		newCfg.SelfProject = oldCfg.SelfProject
	}
	if !reflect.DeepEqual(oldCfg, newCfg) {
		return fmt.Errorf("refusing to write %s: inserting project %s would change existing settings; add it by hand", path, name)
	}
	return nil
}

// errFlowStyle is the error for a config whose projects section is written in flow style, such as
// "projects: {}". Inserting block-style lines into that would produce a different structure, so the
// user adds the project by hand.
func errFlowStyle(path string) error {
	return fmt.Errorf("cannot add to the projects block in %s automatically (flow style); add it by hand", path)
}

// projectsEnd returns the index of the line that a new project entry goes before: the line after
// the last line of the projects section. Blank lines and full-line comments directly above the next
// top-level key belong to that key, so the entry goes above them. When projects is the last key,
// the entry goes after its last line, with any trailing blank lines left at the end of the file.
// projLine is the index of the "projects:" line, and keyIndent is the indentation of the project
// names. A comment indented deeper than keyIndent is never moved over: inside a project it can be
// the text of a block scalar (a run: | script), and inserting above it would cut that scalar short.
func projectsEnd(lines []string, root *yaml.Node, projIdx, projLine, keyIndent int) int {
	at := len(lines)
	if projIdx+2 < len(root.Content) {
		at = root.Content[projIdx+2].Line - 1
	}
	for at-1 > projLine && isBlankOrComment(lines[at-1], keyIndent) {
		at--
	}
	return at
}

// isBlankOrComment reports whether a line may be moved over when adding a project: it holds
// nothing but whitespace, or it is a full-line comment indented no deeper than keyIndent.
func isBlankOrComment(line string, keyIndent int) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return true
	}
	return strings.HasPrefix(t, "#") && leadingSpaces(line) <= keyIndent
}

// leadingSpaces returns the number of spaces at the start of line. YAML forbids tabs in
// indentation, so spaces are the only count that matters.
func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// splitLines splits s into lines that keep their newline characters, so joining them gives back s
// exactly.
func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// insertLines returns lines with block inserted before index at. A last line that lacks a newline
// gets one first, so the inserted text starts on its own line.
func insertLines(lines []string, at int, block []string) []string {
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += "\n"
	}
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	return append(out, lines[at:]...)
}

// entryLines returns the text of one project entry: its name at indent, then its fields two spaces
// further in. Only the fields that were given are written, so the project keeps the defaults for
// the rest.
func entryLines(indent string, p NewProject) ([]string, error) {
	fields := [][2]string{{"repo", p.Repo}}
	for _, f := range [][2]string{{"path", p.Path}, {"default_branch", p.Branch}, {"policy", p.Policy}} {
		if f[1] != "" {
			fields = append(fields, f)
		}
	}
	lines := []string{indent + p.Name + ":\n"}
	for _, f := range fields {
		v, err := yamlScalar(f[1])
		if err != nil {
			return nil, err
		}
		lines = append(lines, indent+"  "+f[0]+": "+v+"\n")
	}
	if p.Instructions != "" {
		// Instructions may span lines, so they are written as a literal block, which keeps the
		// text exactly. A text that block style cannot hold falls back to a quoted string.
		lines = append(lines, instructionLines(indent+"  ", p.Instructions)...)
	}
	return lines, nil
}

// instructionLines returns the "instructions" field at indent. Multi-line text becomes a literal
// block scalar; anything block style would change (leading space, a trailing blank line, control
// characters) is written as a quoted scalar instead.
func instructionLines(indent, text string) []string {
	plain := !strings.ContainsAny(text, "\r\t") && !strings.HasPrefix(text, " ") &&
		!strings.HasPrefix(text, "\n") && !strings.HasSuffix(text, "\n")
	if strings.Contains(text, "\n") && plain {
		lines := []string{indent + "instructions: |-\n"}
		for _, l := range strings.Split(text, "\n") {
			if l == "" {
				lines = append(lines, "\n")
			} else {
				lines = append(lines, indent+"  "+l+"\n")
			}
		}
		return lines
	}
	if !strings.Contains(text, "\n") {
		out, _ := yaml.Marshal(text)
		return []string{indent + "instructions: " + strings.TrimSuffix(string(out), "\n") + "\n"}
	}
	// A JSON string is a valid YAML double-quoted scalar, and keeps every character exactly.
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(text)
	return []string{indent + "instructions: " + buf.String()}
}

// yamlScalar returns s as a YAML value. It stays plain when it reads back unchanged, and is quoted
// when it would not (for example when it contains ": " or starts with a special character).
func yamlScalar(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("%q may not contain a line break", s)
	}
	out, err := yaml.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("quote %q: %w", s, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// mappingValue returns the value stored under key in the mapping node m, or nil when m has no
// such key.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
