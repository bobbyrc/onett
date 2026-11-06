package config

import (
	"fmt"
	"sort"
)

// legacyAgentNames maps the old names of the built-in agents to their job-role names. Every
// place a role name enters (agents and routing in the file, a prompt reference, a task's role,
// and role names already stored in the database) goes through CanonicalAgentName, so a file or
// a stored row that still says "implementer" keeps working.
var legacyAgentNames = map[string]string{
	"orchestrator": "chief-of-staff",
	"implementer":  "senior-engineer",
	"reviewer":     "code-reviewer",
	"triager":      "tech-lead",
	"improver":     "platform-engineer",
}

// CanonicalAgentName returns the current name for an agent name: the job-role name for an old
// built-in name, and any other name unchanged.
func CanonicalAgentName(name string) string {
	if n, ok := legacyAgentNames[name]; ok {
		return n
	}
	return name
}

// LegacyAgentNames returns the old names that map to the given current name, sorted.
func LegacyAgentNames(name string) []string {
	var out []string
	for old, n := range legacyAgentNames {
		if n == name {
			out = append(out, old)
		}
	}
	sort.Strings(out)
	return out
}

// canonicalAgents renames the old-name keys of an agents map. A key whose current name is
// present too is a conflict, returned as a message with the path prefix; the old entry is then
// dropped so the rest of loading can go on.
func canonicalAgents(prefix string, agents map[string]Agent) (map[string]Agent, []string) {
	if len(agents) == 0 {
		return agents, nil
	}
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]Agent, len(agents))
	var conflicts []string
	for _, name := range names {
		canon := CanonicalAgentName(name)
		if canon != name {
			if _, both := agents[canon]; both {
				conflicts = append(conflicts, fmt.Sprintf("%sagents.%s and %sagents.%s: %q is the old name of %q; set only %q",
					prefix, name, prefix, canon, name, canon, canon))
				continue
			}
		}
		out[canon] = agents[name]
	}
	return out, conflicts
}

// canonicalizeNames rewrites old built-in names to current ones in the agents keys and routing
// values, global and per project. Conflicts (both names of one agent present) are kept in
// c.nameConflicts for Validate to report.
func canonicalizeNames(c *Config) {
	c.nameConflicts = nil
	var conflicts []string
	c.Agents, conflicts = canonicalAgents("", c.Agents)
	c.nameConflicts = append(c.nameConflicts, conflicts...)
	for kind, agent := range c.Routing {
		c.Routing[kind] = CanonicalAgentName(agent)
	}
	names := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := c.Projects[name]
		if p == nil {
			continue
		}
		p.Agents, conflicts = canonicalAgents("projects."+name+".", p.Agents)
		c.nameConflicts = append(c.nameConflicts, conflicts...)
		for kind, agent := range p.Routing {
			p.Routing[kind] = CanonicalAgentName(agent)
		}
	}
}

// toolWarnings notes each agent that sets tools on a harness that ignores them. Only
// claude-code honours Agent.Tools.
func toolWarnings(c *Config) []string {
	var out []string
	check := func(path string, a Agent) {
		if len(a.Tools) == 0 {
			return
		}
		if h, ok := c.Harness[a.Harness]; ok && h.Type != "claude-code" {
			out = append(out, fmt.Sprintf("%s.tools: ignored; harness %q (%s) does not take tool rules", path, a.Harness, h.Type))
		}
	}
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check("agents."+name, c.Agents[name])
	}
	return out
}
