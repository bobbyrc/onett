package secrets

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// References finds the environment variable names a config document refers to, mapped to the
// places that refer to them (for example "channels[0].options.bot_token_env"). doc is the
// config as written, before ${NAME} expansion (config.DocumentTree). Two forms count: a string
// value under a key ending in _env that is itself a variable name, and a ${NAME} inside any
// string value.
func References(doc any) map[string][]string {
	refs := map[string][]string{}
	walk(doc, "", refs)
	for name, places := range refs {
		sort.Strings(places)
		refs[name] = slices.Compact(places)
	}
	return refs
}

func walk(v any, path string, refs map[string][]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if s, ok := child.(string); ok && strings.HasSuffix(k, "_env") && ValidName(s) {
				refs[s] = append(refs[s], p)
			}
			walk(child, p, refs)
		}
	case []any:
		for i, child := range x {
			walk(child, fmt.Sprintf("%s[%d]", path, i), refs)
		}
	case string:
		for _, m := range refRe.FindAllStringSubmatch(x, -1) {
			refs[m[1]] = append(refs[m[1]], path)
		}
	}
}
