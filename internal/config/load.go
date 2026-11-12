package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/domain"
	"go.yaml.in/yaml/v3"
)

// envRef matches ${NAME} references. A bare $NAME is left alone, so shell snippets in
// config values such as "vercel rollback $ONETT_ROLLBACK_ID" pass through unchanged.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// EnvRefs returns the names of the environment variables that data references as ${NAME},
// sorted and each once. `onett service install` records them, because a daemon started by
// launchd does not inherit the installing shell's environment.
func EnvRefs(data []byte) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range envRef.FindAllSubmatch(data, -1) {
		if name := string(m[1]); !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Load reads, expands, decodes, defaults and validates the config file at path. Path is
// stored as an absolute path in Config.Path.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config: resolve %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", abs, err)
	}
	cfg, err := Parse(data, abs)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", abs, err)
	}
	return cfg, nil
}

// Parse decodes config bytes as Load does. path is recorded in Config.Path and is used to
// resolve relative agent prompt paths; an empty path resolves them against the working
// directory. An empty document is valid and yields the defaults.
func Parse(data []byte, path string) (*Config, error) {
	marked, err := checkAgentRefs(data)
	if err != nil {
		return nil, err
	}
	expanded, values, err := expandEnv(data)
	if err != nil {
		return nil, err
	}
	if err := checkOneDocument(expanded); err != nil {
		return nil, err
	}
	expanded, warnings := dropRetiredKeys(expanded)
	// dropRetiredKeys can write the file out again, so this reads what the decoder reads.
	if err := checkExpandedAgentText(marked, expanded); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	cfg.Path = path
	cfg.Warnings = warnings
	cfg.expanded = values
	ApplyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Warnings = append(cfg.Warnings, toolWarnings(&cfg)...)
	return &cfg, nil
}

// envRefMarkA and envRefMarkB each stand in for every ${NAME} reference while the file is read
// for the text it gives to agents. They are plain words, so the file parses however a reference
// was written (a bare ${NAME} inside [a, b] is not valid YAML). Text that differs between the
// two readings holds a reference.
const (
	envRefMarkA = "onettEnvRefA"
	envRefMarkB = "onettEnvRefB"
)

// rawAgent is the part of one agent that agentText reads. The fields are nodes, so a value of
// the wrong shape (args: ${ARGS}) is still read.
type rawAgent struct {
	PromptText  yaml.Node `yaml:"prompt_text"`
	Description yaml.Node `yaml:"description"`
	Args        yaml.Node `yaml:"args"`
}

// rawAgentText is the part of the file that agentText reads.
type rawAgentText struct {
	Harnesses map[string]struct {
		Command yaml.Node `yaml:"command"`
		Args    yaml.Node `yaml:"args"`
	} `yaml:"harnesses"`
	Agents   map[string]rawAgent `yaml:"agents"`
	Projects map[string]struct {
		Instructions yaml.Node           `yaml:"instructions"`
		Agents       map[string]rawAgent `yaml:"agents"`
	} `yaml:"projects"`
}

// agentTextReasons says, for each setting agentText reads, why it must not hold a reference.
var agentTextReasons = map[string]string{
	"prompt_text":  "agent prompts are given to agents",
	"description":  "agent descriptions are given to agents",
	"instructions": "project instructions are given to agents",
	"args":         "args are put on the command line of agents",
	"command":      "everything after the program name is put on the command line of agents",
}

// agentText reads data as YAML and returns the text of every setting that reaches an agent,
// keyed by the setting's dotted name: an agent's inline prompt, description and args (global and
// per project), a harness's args and the arguments of its command, and a project's
// instructions. A setting whose parent has the wrong shape is left out. The error is non-nil
// only when data is not YAML at all.
func agentText(data []byte) (map[string]string, error) {
	var raw rawAgentText
	var shape *yaml.TypeError
	if err := yaml.Unmarshal(data, &raw); err != nil && !errors.As(err, &shape) {
		return nil, err
	}
	text := map[string]string{}
	put := func(field string, n *yaml.Node) {
		var b strings.Builder
		writeScalars(&b, n, map[*yaml.Node]bool{})
		if b.Len() > 0 {
			text[field] = b.String()
		}
	}
	agents := func(prefix string, agents map[string]rawAgent) {
		for name, a := range agents {
			at := prefix + "agents." + name
			put(at+".prompt_text", &a.PromptText)
			put(at+".description", &a.Description)
			put(at+".args", &a.Args)
		}
	}
	for name, h := range raw.Harnesses {
		put("harnesses."+name+".args", &h.Args)
		// The first element of command is the program to run, often a path under ${HOME}. The
		// rest are arguments, like args.
		command := &h.Command
		if command.Kind == yaml.AliasNode && command.Alias != nil {
			command = command.Alias
		}
		if command.Kind == yaml.SequenceNode && len(command.Content) > 1 {
			put("harnesses."+name+".command", &yaml.Node{Kind: yaml.SequenceNode, Content: command.Content[1:]})
		}
	}
	agents("", raw.Agents)
	for name, p := range raw.Projects {
		put("projects."+name+".instructions", &p.Instructions)
		agents("projects."+name+".", p.Agents)
	}
	return text, nil
}

// writeScalars writes every scalar at or below n to b, following aliases. seen stops an anchor
// that contains itself from recursing for ever.
func writeScalars(b *strings.Builder, n *yaml.Node, seen map[*yaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	if n.Kind == yaml.AliasNode {
		writeScalars(b, n.Alias, seen)
		return
	}
	if n.Kind == yaml.ScalarNode {
		b.WriteString(n.Value)
		b.WriteByte(0)
	}
	for _, c := range n.Content {
		writeScalars(b, c, seen)
	}
}

// changedAgentText returns the names of the settings whose text differs between a and b, sorted.
func changedAgentText(a, b map[string]string) []string {
	var names []string
	for name, text := range a {
		if other, ok := b[name]; !ok || other != text {
			names = append(names, name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// refuseAgentRefs returns one error for each named setting, or nil for none. A name that itself
// came from a reference is shown with ${NAME} in its place.
func refuseAgentRefs(fields []string) error {
	unmark := strings.NewReplacer(envRefMarkA, "${NAME}", envRefMarkB, "${NAME}")
	seen := map[string]bool{}
	var errs []error
	for _, field := range fields {
		field = unmark.Replace(field)
		if seen[field] {
			continue
		}
		seen[field] = true
		reason := agentTextReasons[field[strings.LastIndex(field, ".")+1:]]
		errs = append(errs, fmt.Errorf("%s: must not contain ${NAME} references; %s, which never get credentials. Remove the reference; to hand agents the variable itself, name it in the harness's inherit_env", field, reason))
	}
	return errors.Join(errs...)
}

// checkAgentRefs refuses a ${NAME} reference in a setting whose text reaches an agent (see
// agentText). Prompts, descriptions and instructions are given to agents verbatim and arguments
// are put on their command line, and an agent never gets credentials, so the reference is
// refused whether or not the variable is set. It reads the bytes before expandEnv runs, so the
// message names the setting rather than an unset variable. It returns the text it read, for
// checkExpandedAgentText, or nil when the file does not parse with its references masked.
func checkAgentRefs(data []byte) (map[string]string, error) {
	marked, err := agentText(envRef.ReplaceAllLiteral(data, []byte(envRefMarkA)))
	if err != nil {
		return nil, nil
	}
	other, err := agentText(envRef.ReplaceAllLiteral(data, []byte(envRefMarkB)))
	if err != nil {
		return nil, nil
	}
	return marked, refuseAgentRefs(changedAgentText(marked, other))
}

// checkExpandedAgentText is the second half of the check, run on the bytes Parse decodes, with
// the references filled in: the text that reaches agents must be what checkAgentRefs read. It
// differs when a reference stood for a setting's name or a whole section, or when its value
// held YAML of its own. A setting that exists only now may take its name from a variable's
// value, so it is refused without being named. A file that parses only once its references are
// filled in cannot be checked, so it is refused. A file that does not parse at all is left for
// Parse's decode to report.
func checkExpandedAgentText(marked map[string]string, expanded []byte) error {
	text, err := agentText(expanded)
	if err != nil {
		return nil
	}
	if marked == nil {
		return errors.New("a ${NAME} reference stands where YAML syntax is expected, so onett cannot check that no reference reaches an agent; write references only inside values")
	}
	var added error
	var changed []string
	for _, field := range changedAgentText(marked, text) {
		if _, ok := marked[field]; ok {
			changed = append(changed, field)
		} else {
			added = errors.New("a ${NAME} reference adds a setting whose text is given to agents (a prompt_text, description, args, command or instructions), and agents never get credentials; write such settings out in the file itself")
		}
	}
	return errors.Join(added, refuseAgentRefs(changed))
}

// checkOneDocument refuses a file that holds more than one YAML document. Parse decodes only the
// first, so settings after a second "---" line would be dropped without a word. A file that does
// not parse is left for Parse's decode to report.
func checkOneDocument(data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(new(yaml.Node)); err != nil {
		return nil
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return errors.New(`the file holds more than one YAML document; onett reads only one, so move every setting into the first document and remove the extra "---" line`)
	}
	return nil
}

// minSecretLen is the shortest substituted value that Redacted masks. Shorter values are too
// likely to occur by chance inside other config text, and masking them would garble the output.
const minSecretLen = 6

// expandEnv replaces ${NAME} with the value of the environment variable NAME. A variable that is
// set to the empty string expands to nothing. A variable that is not set is an error, so a typo
// or a missing secret fails at startup instead of becoming an empty value. The error names each
// unset variable once, in sorted order. On success it also returns each distinct substituted
// value of at least minSecretLen characters, so that Redacted can mask them wherever they appear.
func expandEnv(data []byte) ([]byte, []string, error) {
	var missing []string
	seen := map[string]bool{}
	var values []string
	seenValue := map[string]bool{}
	out := envRef.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(envRef.FindSubmatch(match)[1])
		value, ok := os.LookupEnv(name)
		if !ok {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
			return nil
		}
		if utf8.RuneCountInString(value) >= minSecretLen && !seenValue[value] {
			seenValue[value] = true
			values = append(values, value)
		}
		return []byte(value)
	})
	if len(missing) == 0 {
		return out, values, nil
	}
	sort.Strings(missing)
	errs := make([]error, len(missing))
	for i, name := range missing {
		errs[i] = fmt.Errorf("undefined environment variable ${%s}", name)
	}
	return nil, nil, errors.Join(errs...)
}

// RollbackEnabled reports whether a failed verify window rolls the release back. Default true.
func (d *Deploy) RollbackEnabled() bool {
	return d.AutoRollback == nil || *d.AutoRollback
}

// FrictionAutoFile reports whether friction reports are filed as GitHub issues. Default true.
func (l Loops) FrictionAutoFile() bool {
	return l.Friction.AutoFile == nil || *l.Friction.AutoFile
}

// RunShimOn reports whether agents run behind a per-run shim: true unless run_shim is false.
func (l Limits) RunShimOn() bool { return l.RunShim == nil || *l.RunShim }

// RetroOn reports whether the retro loop runs for project. An explicit enabled setting wins.
// Unset, it runs only for selfProject, because a retrospective spends agent time.
func (l Loops) RetroOn(project, selfProject string) bool {
	if l.Retro.Enabled != nil {
		return *l.Retro.Enabled
	}
	return selfProject != "" && project == selfProject
}

// SelfUpdateOn reports whether the self_update loop runs for project: only for selfProject (the
// loop replaces the running daemon), unless enabled is false, and only when a command is known
// (set, or inferred by ApplySelfUpdateInference). An unset enabled means on.
func (l Loops) SelfUpdateOn(project, selfProject string) bool {
	if selfProject == "" || project != selfProject || strings.TrimSpace(l.SelfUpdate.Command) == "" {
		return false
	}
	return l.SelfUpdate.Enabled == nil || *l.SelfUpdate.Enabled
}

// LoopsFor returns the loop settings that apply to project: the global Loops with each
// field the project overrides applied on top. Unset override fields keep the global value.
func (c *Config) LoopsFor(project string) Loops {
	out := c.Loops
	p := c.Projects[project]
	if p == nil || p.Loops == nil {
		return out
	}
	ov := p.Loops

	if ov.IssueIntake.Enabled != nil {
		out.IssueIntake.Enabled = ov.IssueIntake.Enabled
	}
	if ov.IssueIntake.Every.Duration != 0 {
		out.IssueIntake.Every = ov.IssueIntake.Every
	}
	if ov.IssueIntake.Label != "" {
		out.IssueIntake.Label = ov.IssueIntake.Label
	}

	if ov.PRShepherd.Enabled != nil {
		out.PRShepherd.Enabled = ov.PRShepherd.Enabled
	}
	if ov.PRShepherd.Every.Duration != 0 {
		out.PRShepherd.Every = ov.PRShepherd.Every
	}

	if ov.DeployWatch.Enabled != nil {
		out.DeployWatch.Enabled = ov.DeployWatch.Enabled
	}
	if ov.DeployWatch.Every.Duration != 0 {
		out.DeployWatch.Every = ov.DeployWatch.Every
	}

	if ov.ProdWatch.Enabled != nil {
		out.ProdWatch.Enabled = ov.ProdWatch.Enabled
	}
	if ov.ProdWatch.Every.Duration != 0 {
		out.ProdWatch.Every = ov.ProdWatch.Every
	}
	if ov.ProdWatch.MinLevel != "" {
		out.ProdWatch.MinLevel = ov.ProdWatch.MinLevel
	}
	if ov.ProdWatch.MinCount != 0 {
		out.ProdWatch.MinCount = ov.ProdWatch.MinCount
	}

	if ov.Friction.Enabled != nil {
		out.Friction.Enabled = ov.Friction.Enabled
	}
	if ov.Friction.Every.Duration != 0 {
		out.Friction.Every = ov.Friction.Every
	}
	if ov.Friction.AutoFile != nil {
		out.Friction.AutoFile = ov.Friction.AutoFile
	}
	if ov.Friction.AutoQueue {
		out.Friction.AutoQueue = true
	}
	if ov.Friction.Label != "" {
		out.Friction.Label = ov.Friction.Label
	}
	if ov.Friction.MinCount != 0 {
		out.Friction.MinCount = ov.Friction.MinCount
	}

	if ov.Retro.Enabled != nil {
		out.Retro.Enabled = ov.Retro.Enabled
	}
	if ov.Retro.Every.Duration != 0 {
		out.Retro.Every = ov.Retro.Every
	}
	if ov.Retro.Lookback.Duration != 0 {
		out.Retro.Lookback = ov.Retro.Lookback
	}

	if ov.ExecIntake.Enabled != nil {
		out.ExecIntake.Enabled = ov.ExecIntake.Enabled
	}
	if ov.ExecIntake.Every.Duration != 0 {
		out.ExecIntake.Every = ov.ExecIntake.Every
	}

	if ov.SelfUpdate.Enabled != nil {
		out.SelfUpdate.Enabled = ov.SelfUpdate.Enabled
	}
	if ov.SelfUpdate.Every.Duration != 0 {
		out.SelfUpdate.Every = ov.SelfUpdate.Every
	}
	if ov.SelfUpdate.MinHealthy.Duration != 0 {
		out.SelfUpdate.MinHealthy = ov.SelfUpdate.MinHealthy
	}
	if ov.SelfUpdate.Command != "" {
		out.SelfUpdate.Command = ov.SelfUpdate.Command
	}
	if ov.Goals.Enabled != nil {
		out.Goals.Enabled = ov.Goals.Enabled
	}
	if ov.Goals.Every.Duration != 0 {
		out.Goals.Every = ov.Goals.Every
	}
	if ov.Heartbeat.Enabled != nil {
		out.Heartbeat.Enabled = ov.Heartbeat.Enabled
	}
	if ov.Heartbeat.Every.Duration != 0 {
		out.Heartbeat.Every = ov.Heartbeat.Every
	}
	if ov.Heartbeat.QuietHours != "" {
		out.Heartbeat.QuietHours = ov.Heartbeat.QuietHours
	}
	if ov.Heartbeat.StaleQueued.Duration != 0 {
		out.Heartbeat.StaleQueued = ov.Heartbeat.StaleQueued
	}
	if ov.Heartbeat.EscalateAfter != 0 {
		out.Heartbeat.EscalateAfter = ov.Heartbeat.EscalateAfter
	}
	return out
}

// AgentsFor returns the agent roles that apply to project: the global roles with the project's
// overrides applied. A set field of an override (Harness, Model, Effort, PermissionMode, Prompt, Args, Description,
// Title, Context, Tools)
// replaces the global role's field; a name the global map lacks adds a project-only role. The
// map is fresh, so the caller may change it. An empty or unconfigured project gets the global
// roles.
func (c *Config) AgentsFor(project string) map[string]Agent {
	out := make(map[string]Agent, len(c.Agents))
	for name, a := range c.Agents {
		out[name] = a
	}
	p := c.Projects[project]
	if p == nil {
		return out
	}
	for name, ov := range p.Agents {
		base, ok := out[name]
		if !ok {
			ov.Origin = OriginCustom
			out[name] = ov
			continue
		}
		if ov.Harness != "" {
			base.Harness = ov.Harness
		}
		if ov.Model != "" {
			base.Model = ov.Model
		}
		if ov.Effort != "" {
			base.Effort = ov.Effort
		}
		if ov.PermissionMode != "" {
			base.PermissionMode = ov.PermissionMode
		}
		// Prompt and PromptText replace each other, so the agent never has both.
		if ov.Prompt != "" {
			base.Prompt = ov.Prompt
			base.PromptText = ""
		}
		if ov.PromptText != "" {
			base.PromptText = ov.PromptText
			base.Prompt = ""
		}
		if len(ov.Args) > 0 {
			base.Args = ov.Args
		}
		if ov.Description != "" {
			base.Description = ov.Description
		}
		if ov.Title != "" {
			base.Title = ov.Title
		}
		if len(ov.Context) > 0 {
			base.Context = ov.Context
		}
		if len(ov.Tools) > 0 {
			base.Tools = ov.Tools
		}
		if ov.Disabled {
			base.Disabled = true
		}
		out[name] = base
	}
	return out
}

// AgentFor picks the agent for a task from the global roles. See AgentForProject.
func (c *Config) AgentFor(kind domain.TaskKind, role string) (string, Agent, error) {
	return c.AgentForProject("", kind, role)
}

// AgentForProject picks the agent for a task of project. A non-empty role names the agent
// directly and must exist in AgentsFor(project). Otherwise the agent is the one routed to kind:
// the project's Routing entry if it has one, else the global Routing entry. It returns the
// agent's name too.
func (c *Config) AgentForProject(project string, kind domain.TaskKind, role string) (string, Agent, error) {
	agents := c.AgentsFor(project)
	if role != "" {
		role = CanonicalAgentName(role)
		a, ok := agents[role]
		if !ok {
			return "", Agent{}, fmt.Errorf("config: unknown agent role %q", role)
		}
		if a.Disabled {
			return "", Agent{}, fmt.Errorf("config: agent %q is disabled", role)
		}
		return role, a, nil
	}
	name, ok := c.routeFor(project, kind)
	if !ok {
		return "", Agent{}, fmt.Errorf("config: no agent routed for task kind %q", kind)
	}
	a, ok := agents[name]
	if !ok {
		return "", Agent{}, fmt.Errorf("config: routing %q names unknown agent %q", kind, name)
	}
	if a.Disabled {
		return "", Agent{}, fmt.Errorf("config: agent %q is disabled", name)
	}
	return name, a, nil
}

// routeFor returns the agent name routed to kind for project, preferring the project's routing.
func (c *Config) routeFor(project string, kind domain.TaskKind) (string, bool) {
	if p := c.Projects[project]; p != nil {
		if name, ok := p.Routing[string(kind)]; ok {
			return name, true
		}
	}
	name, ok := c.Routing[string(kind)]
	return name, ok
}

// PolicyFor returns the policy for project: its named policy, falling back to "default".
func (c *Config) PolicyFor(project string) Policy {
	name := "default"
	if p := c.Projects[project]; p != nil && p.Policy != "" {
		name = p.Policy
	}
	if pol, ok := c.Policies[name]; ok {
		return pol
	}
	return c.Policies["default"]
}

// ProjectNames returns the configured project names in sorted order.
func (c *Config) ProjectNames() []string {
	names := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
