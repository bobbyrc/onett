package config

import "testing"

func findNode(t *testing.T, n SchemaNode, path ...string) SchemaNode {
	t.Helper()
	for _, f := range n.Fields {
		if f.Key == path[0] {
			if len(path) == 1 {
				return f
			}
			return findNode(t, f, path[1:]...)
		}
	}
	t.Fatalf("no setting %v", path)
	return SchemaNode{}
}

func TestSchemaShowsBoolDefaults(t *testing.T) {
	root := Schema()
	for _, p := range [][]string{
		{"loops", "pr_shepherd", "enabled"},
		{"loops", "issue_intake", "enabled"},
		{"loops", "friction", "enabled"},
		{"loops", "friction", "auto_file"},
	} {
		if d := findNode(t, root, p...).Default; d == nil || !*d {
			t.Errorf("%v: Default = %v, want true", p, d)
		}
	}
	// Retro and self_update work out their own default, so the schema shows none.
	for _, p := range [][]string{
		{"loops", "retro", "enabled"},
		{"loops", "self_update", "enabled"},
		{"loops", "friction", "auto_queue"},
	} {
		if d := findNode(t, root, p...).Default; d != nil {
			t.Errorf("%v: Default = %v, want none", p, *d)
		}
	}
	// auto_rollback lives in a project's deploy section, under the projects map.
	projects := findNode(t, root, "projects")
	deploy := findNode(t, *projects.Item, "deploy", "auto_rollback")
	if deploy.Default == nil || !*deploy.Default {
		t.Errorf("auto_rollback Default = %v, want true", deploy.Default)
	}
}
