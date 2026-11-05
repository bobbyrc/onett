package config

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// retiredKeys are settings that no longer exist. An existing file may still set one, so the key
// is removed before the strict decode (which refuses unknown keys) and a warning is recorded
// instead. Each path is a chain of YAML keys; "*" matches every key of a map.
var retiredKeys = []struct {
	path []string
	why  string
}{
	{[]string{"loops", "self_update", "max_defer"}, "the daemon now updates as soon as a release has been healthy for min_healthy"},
	{[]string{"projects", "*", "loops", "self_update", "max_defer"}, "the daemon now updates as soon as a release has been healthy for min_healthy"},
}

// dropRetiredKeys removes retired keys from the YAML document in data and returns the result with
// a one-line warning per key it removed. A document that does not parse is returned unchanged,
// for the strict decode to report.
func dropRetiredKeys(data []byte) ([]byte, []string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return data, nil
	}
	var warnings []string
	for _, r := range retiredKeys {
		for _, where := range removeKey(doc.Content[0], r.path, "") {
			warnings = append(warnings, fmt.Sprintf("config: %s is retired and ignored: %s", where, r.why))
		}
	}
	if len(warnings) == 0 {
		return data, nil
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return data, nil
	}
	return out, warnings
}

// removeKey deletes the key at path below n and returns the dotted path of each one removed.
func removeKey(n *yaml.Node, path []string, prefix string) []string {
	if n.Kind != yaml.MappingNode || len(path) == 0 {
		return nil
	}
	var removed []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if path[0] != "*" && path[0] != k {
			continue
		}
		here := prefix + k
		if len(path) == 1 {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			removed = append(removed, here)
			i -= 2
			continue
		}
		removed = append(removed, removeKey(n.Content[i+1], path[1:], here+".")...)
	}
	return removed
}
