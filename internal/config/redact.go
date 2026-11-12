package config

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// redacted replaces the value of every secret-looking setting.
const redacted = "***"

// secretWords mark an Options key whose value is replaced when the config is shown.
var secretWords = []string{"token", "secret", "password", "key"}

// Redacted returns a deep copy of c that is safe to show: every environment variable value,
// every option whose name suggests a secret, the ntfy topic, every value substituted for a ${NAME}
// reference anywhere in the config, and credentials written as literals in commands and URLs (see
// maskLiteralSecrets) are replaced with "***". c is not modified.
func (c *Config) Redacted() *Config {
	data, err := json.Marshal(c)
	if err != nil {
		// Config only holds JSON-safe values, so this cannot happen for a loaded config.
		panic("config: redact: " + err.Error())
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		panic("config: redact: " + err.Error())
	}
	out.Path = c.Path
	for name, p := range out.Projects {
		if p == nil {
			continue
		}
		p.Name = name
		redactEnv(p.Env)
		if p.Deploy != nil {
			redactOptions(p.Deploy.Options)
		}
		for i := range p.Observe {
			redactOptions(p.Observe[i].Options)
		}
	}
	for name, h := range out.Harness {
		redactEnv(h.Env)
		out.Harness[name] = h
	}
	for i := range out.Notify {
		redactOptions(out.Notify[i].Options)
		// On ntfy the topic name is all that keeps others from reading and posting.
		if out.Notify[i].Type == "ntfy" && out.Notify[i].Options["topic"] != nil {
			out.Notify[i].Options["topic"] = redacted
		}
	}
	for i := range out.Channels {
		redactOptions(out.Channels[i].Options)
	}
	out = maskStrings(out, c.expanded)
	return &out
}

// redactEnv replaces every value in env in place.
func redactEnv(env map[string]string) {
	for k := range env {
		env[k] = redacted
	}
}

// redactOptions replaces the value of each option whose key contains a secret word, in place.
func redactOptions(opts map[string]any) {
	for k := range opts {
		lower := strings.ToLower(k)
		for _, w := range secretWords {
			if strings.Contains(lower, w) {
				opts[k] = redacted
				break
			}
		}
	}
}

// secretName matches a name that contains one of secretWords, such as VERCEL_TOKEN or api-key.
var secretName = `[A-Za-z0-9_.-]*(?:` + strings.Join(secretWords, "|") + `)[A-Za-z0-9_-]*`

// The patterns maskLiteralSecrets looks for. In those with two groups, group 1 is the text to
// keep and group 2 is the value to mask.
var (
	// scheme://user:password@host. Everything between "://" and the last "@" before the path
	// goes: the user part can itself be a token (https://ghp_...@github.com) and a password can
	// hold an "@".
	urlCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/'"]+@`)
	// Authorization: Basic abc, Authorization: Bearer abc, Authorization: abc.
	authHeader = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:[A-Za-z]+\s+)?)[^\s"']+`)
	// -u user:password and --user user:password, as curl takes them. A quoted value goes whole.
	userFlag = regexp.MustCompile(`((?:^|\s)(?:-u|--user)[\s=]+)(?:"[^"]*"|'[^']*'|[^\s:"']+:[^\s"']+)`)
	// Chat webhook addresses, whose path is the credential.
	webhookURL = regexp.MustCompile(`((?:hooks\.slack\.com/services|discord(?:app)?\.com/api/webhooks)/)[^\s"']+`)
	// A flag on its own whose name contains a secret word (--api-key): in an argument list its
	// value is the next element.
	secretFlagName = regexp.MustCompile(`(?i)^--?` + secretName + `$`)
	// NAME=value, "Name: value" and "name":"value": VERCEL_TOKEN=abc, --password=abc,
	// ?api_key=abc, X-Api-Key: abc.
	secretAssignment = regexp.MustCompile(`(?i)(` + secretName + `["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s"'&;|]+)`)
	// --name value. A value that starts with "-" is the next flag, not a value.
	secretFlag = regexp.MustCompile(`(?i)((?:^|\s)--?` + secretName + `\s+)("[^"]*"|'[^']*'|[^\s"'&;|-][^\s"'&;|]*)`)
	// Bearer abc, wherever it stands.
	bearerToken = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]{8,}`)
	// Tokens that can be told by their prefix wherever they stand: GitHub, OpenAI and Anthropic,
	// Stripe, Slack, GitLab, AWS.
	prefixedToken = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|[sr]k_(?:live|test)_[A-Za-z0-9]{10,}|xox[abprs]-[A-Za-z0-9-]{10,}|glpat-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16})`)
	// shellReference matches a value that is nothing but a shell variable ($NAME, "${NAME}") or
	// that starts a command substitution ($(cmd)): a name, not a secret, and useful to see. A
	// single-quoted value is never one, because the shell does not expand it, and neither is
	// ${NAME:-default}, because the default is a literal.
	shellReference = regexp.MustCompile(`^"?\$(?:\(|[A-Za-z_][A-Za-z0-9_]*"?$|\{[A-Za-z_][A-Za-z0-9_]*\}"?$)`)
)

// maskLiteralSecrets replaces credentials written as literals in a command or URL with "***":
// the user and password of a URL, an Authorization value, a -u user:password pair, a chat webhook
// address, the value
// after a name that contains one of secretWords, a bearer token, and a token with a well-known
// prefix. It is best effort: it finds a secret by the shape of the text around it, so one passed
// as a bare argument is not found.
func maskLiteralSecrets(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}"+redacted+"@")
	s = authHeader.ReplaceAllString(s, "${1}"+redacted)
	s = userFlag.ReplaceAllString(s, "${1}"+redacted)
	s = webhookURL.ReplaceAllString(s, "${1}"+redacted)
	s = maskValue(secretAssignment, s)
	s = maskValue(secretFlag, s)
	s = bearerToken.ReplaceAllString(s, "${1}"+redacted)
	return prefixedToken.ReplaceAllString(s, redacted)
}

// maskValue replaces group 2 of every match of re in s with "***", except a shell reference.
func maskValue(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(match string) string {
		groups := re.FindStringSubmatch(match)
		if shellReference.MatchString(groups[2]) {
			return match
		}
		return groups[1] + redacted
	})
}

// commandSections are the top-level sections of the config, and projectCommandSections the
// sections of each project, whose strings are commands and URLs. A secret typed into one as a
// literal is masked. Harnesses are handled by maskHarnesses. Agents and project instructions are
// left out: they are prose and arguments that the agent editor reads back from this copy.
var (
	commandSections        = []string{"loops", "notify", "channels"}
	projectCommandSections = []string{"deploy", "observe", "setup", "checks", "intake", "loops"}
)

// maskStrings replaces every occurrence of each value in every string of cfg with "***", and masks
// literal credentials in the sections that hold commands and URLs. The config is walked as generic
// JSON, so values inside commands, args, URLs and instructions are found too. Path and project
// names are kept as they are: they come from the file, not from substituted text. The result is a
// fresh Config.
func maskStrings(cfg Config, values []string) Config {
	// Longest first, so a value that contains another one is masked whole.
	sorted := append([]string(nil), values...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	pairs := make([]string, 0, 2*len(sorted))
	for _, v := range sorted {
		pairs = append(pairs, v, redacted)
	}
	replacer := strings.NewReplacer(pairs...)

	data, err := json.Marshal(cfg)
	if err != nil {
		panic("config: redact: " + err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep numbers exact rather than as float64
	var doc any
	if err := dec.Decode(&doc); err != nil {
		panic("config: redact: " + err.Error())
	}
	doc = replaceStrings(doc, replacer.Replace)
	if root, ok := doc.(map[string]any); ok {
		maskSections(root, commandSections)
		maskHarnesses(root["harnesses"])
		projects, _ := root["projects"].(map[string]any)
		for _, p := range projects {
			if project, ok := p.(map[string]any); ok {
				maskSections(project, projectCommandSections)
			}
		}
	}
	masked, err := json.Marshal(doc)
	if err != nil {
		panic("config: redact: " + err.Error())
	}
	var out Config
	if err := json.Unmarshal(masked, &out); err != nil {
		// A substituted value that sat inside a typed non-string field (such as a duration)
		// can make the masked text unreadable as that type.
		panic("config: redact: " + err.Error())
	}
	out.Path = cfg.Path
	for name, p := range out.Projects {
		if p != nil {
			p.Name = name
		}
	}
	return out
}

// maskSections applies maskLiteralSecrets to every string under the named keys of m.
func maskSections(m map[string]any, keys []string) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			m[k] = replaceStrings(v, maskLiteralSecrets)
		}
	}
}

// maskHarnesses masks literal credentials in each harness's args and in the arguments of its
// command. The first element of command is the program path, which GET /agents looks up, so it
// is kept as it is.
func maskHarnesses(v any) {
	harnesses, _ := v.(map[string]any)
	for _, h := range harnesses {
		harness, _ := h.(map[string]any)
		if args, ok := harness["args"].([]any); ok {
			maskArgs(args)
		}
		if command, ok := harness["command"].([]any); ok && len(command) > 1 {
			maskArgs(command[1:])
		}
	}
}

// maskArgs masks literal credentials in an argument list, in place. A value is often its own
// element (["--api-key", "abc"]), so the element after a flag whose name contains a secret word
// is masked whole, unless it is another flag or a shell reference.
func maskArgs(args []any) {
	for i, v := range args {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if i > 0 && !strings.HasPrefix(s, "-") && !shellReference.MatchString(s) {
			if flag, ok := args[i-1].(string); ok && secretFlagName.MatchString(flag) {
				args[i] = redacted
				continue
			}
		}
		args[i] = maskLiteralSecrets(s)
	}
}

// replaceStrings applies mask to every string in the generic JSON value v, in place, and returns v.
func replaceStrings(v any, mask func(string) string) any {
	switch t := v.(type) {
	case string:
		return mask(t)
	case []any:
		for i := range t {
			t[i] = replaceStrings(t[i], mask)
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = replaceStrings(t[k], mask)
		}
		return t
	default:
		return v
	}
}
