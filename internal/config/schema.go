package config

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Help text is read from the comments of these source files, so the UI's help can never drift
// from the code's own documentation. config.go holds the settings, channels.go the channel list
// entry and options.go the per-type option structs.
//
//go:embed config.go
var configSource string

//go:embed channels.go
var channelsSource string

//go:embed options.go
var optionsSource string

// SchemaNode describes one setting of onett.yaml for the web UI's editor.
type SchemaNode struct {
	Key    string       `json:"key"`              // YAML key; "" for map values and list elements
	Type   string       `json:"type"`             // section | map | list | string | int | float | bool | duration | variant (an options map whose fields depend on the sibling type) | any (a free-form value that the UI edits as YAML text)
	Help   string       `json:"help,omitempty"`   // the field's doc comment from config.go, as one paragraph
	Fields []SchemaNode `json:"fields,omitempty"` // section: fields in declaration order
	Item   *SchemaNode  `json:"item,omitempty"`   // map and list: the value or element schema
	// Enum lists the allowed values of a string setting, sorted. The UI shows a dropdown.
	Enum []string `json:"enum,omitempty"`
	// SecretEnv marks a string that names an environment variable holding a secret, never the secret.
	SecretEnv bool `json:"secret_env,omitempty"`
	// Discriminator (variant only) is the sibling key whose value picks the variant: "type".
	Discriminator string `json:"discriminator,omitempty"`
	// Default is the value the setting has when the file leaves it out: a string, a number, true
	// or a duration such as "15m". It is read from ApplyDefaults, so it cannot drift from the
	// code. Absent when the default is empty or zero, depends on context (an agent's harness)
	// or is a list or map.
	Default any `json:"default,omitempty"`
	// Variants (variant only) maps each value of the discriminator to a section of its options.
	Variants map[string]SchemaNode `json:"variants,omitempty"`
}

var (
	schemaOnce sync.Once
	schemaRoot SchemaNode
)

// Schema describes every setting in Config. It is built once, from the struct's reflected
// types and the doc comments in config.go. A field of a type it does not know panics, so a new
// config type cannot be left out of the editor silently.
func Schema() SchemaNode {
	schemaOnce.Do(func() {
		help := map[string]string{}
		for name, src := range map[string]string{"config.go": configSource, "channels.go": channelsSource, "options.go": optionsSource} {
			for k, v := range parseHelp(name, src) {
				help[k] = v
			}
		}
		b := schemaBuilder{help: help}
		schemaRoot = SchemaNode{Type: "section", Fields: b.structNode(reflect.TypeOf(Config{}), "Config", defaultsConfig()).Fields}
		// Loop.Enabled defaults to on, but retro and self_update work it out (see RetroOn and
		// SelfUpdateOn), so a flat "default: on" would be wrong for them.
		// A project can override the loops too, so clear them under projects as well.
		var projects SchemaNode
		for _, f := range schemaRoot.Fields {
			if f.Key == "projects" {
				projects = f
			}
		}
		for _, loop := range []string{"retro", "self_update"} {
			clearDefault(schemaRoot, "loops", loop, "enabled")
			clearDefault(*projects.Item, "loops", loop, "enabled")
		}
	})
	return schemaRoot
}

// clearDefault removes the Default of the setting at path under n.
func clearDefault(n SchemaNode, path ...string) {
	for i := range n.Fields {
		if n.Fields[i].Key != path[0] {
			continue
		}
		if len(path) == 1 {
			n.Fields[i].Default = nil
			return
		}
		clearDefault(n.Fields[i], path[1:]...)
		return
	}
	panic(fmt.Sprintf("config schema: no setting %s", strings.Join(path, ".")))
}

// sampleName is the one map key the defaults config uses, so the defaults a project or policy
// gets can be read from it. Other maps (agents, harnesses) have no such key and show no defaults.
const sampleName = "x"

// defaultsConfig is an empty Config run through ApplyDefaults, with one project and one policy
// entry so the defaults applied to every entry show up too.
func defaultsConfig() reflect.Value {
	c := Config{
		Projects: map[string]*Project{sampleName: {Deploy: &Deploy{}}},
		Policies: map[string]Policy{sampleName: {}},
	}
	ApplyDefaults(&c)
	// A project's path depends on its name, so it is not a default worth showing.
	c.Projects[sampleName].Path = ""
	// Show "~" as the file would, not the home directory of the machine that built the schema.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		c.Server.DataDir = strings.Replace(c.Server.DataDir, home, "~", 1)
	}
	return reflect.ValueOf(c)
}

// defaultOf returns the default a scalar value v stands for, or nil when v is unknown, zero or
// not a scalar. A duration is written as in the file: "15m", not "15m0s".
func defaultOf(v reflect.Value) any {
	if !v.IsValid() || v.IsZero() {
		return nil
	}
	if v.Type() == durationType {
		return shortDuration(v.Interface().(Duration).Duration)
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Bool:
		return v.Bool()
	}
	return nil
}

// shortDuration formats d without the zero units time.Duration.String adds: "15m", "2h", "30s".
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// fieldValue returns the value of struct field i of v, or an invalid Value when v is unknown or
// a nil pointer.
func fieldValue(v reflect.Value, i int) reflect.Value {
	if !v.IsValid() {
		return reflect.Value{}
	}
	return v.Field(i)
}

// itemValue returns the sample entry of a map value, or an invalid Value when there is none.
func itemValue(v reflect.Value) reflect.Value {
	if !v.IsValid() || v.Kind() != reflect.Map {
		return reflect.Value{}
	}
	return v.MapIndex(reflect.ValueOf(sampleName))
}

// durationType is config.Duration, which is written as "15m" and is not an ordinary struct.
var durationType = reflect.TypeOf(Duration{})

type schemaBuilder struct {
	help map[string]string // "Type.Field" -> help text
}

// structNode builds a section from t's fields. base names the struct for help lookups; an
// anonymous struct has no name of its own, so its caller passes one such as "Loops.IssueIntake".
// v is the defaults config's value of the struct, or invalid when it has no uniform defaults.
func (b schemaBuilder) structNode(t reflect.Type, base string, v reflect.Value) SchemaNode {
	if t.Name() != "" {
		base = t.Name()
	}
	var fields []SchemaNode
	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if hasOption(opts, "inline") {
			if sf.Type.Kind() != reflect.Struct {
				panic(fmt.Sprintf("config schema: %s.%s is inline but not a struct", base, sf.Name))
			}
			fields = append(fields, b.structNode(sf.Type, base, fieldValue(v, i)).Fields...)
			continue
		}
		if name == "" {
			name = strings.ToLower(sf.Name)
		}
		helpKey := base + "." + sf.Name
		node := b.typeNode(sf.Type, helpKey, fieldValue(v, i))
		if kind, ok := optionKindByStruct[t.Name()]; ok && name == "options" && node.Type == "any" && len(optionTypes[kind]) > 0 {
			node = b.variantNode(kind)
		}
		if tag := sf.Tag.Get("onett"); tag != "" {
			if tag != "secret_env" || node.Type != "string" {
				panic(fmt.Sprintf("config schema: %s has onett tag %q, want secret_env on a string", helpKey, tag))
			}
			node.SecretEnv = true
		}
		if tag := sf.Tag.Get("default"); tag != "" {
			d, err := strconv.ParseBool(tag)
			if err != nil || node.Type != "bool" {
				panic(fmt.Sprintf("config schema: %s has default tag %q, want a bool on a bool setting", helpKey, tag))
			}
			node.Default = d
		}
		node.Key = name
		node.Help = leadWithYAMLKey(b.help[helpKey], sf.Name, name)
		fields = append(fields, node)
	}
	if kind, ok := optionKindByStruct[t.Name()]; ok {
		// The sibling `type` selects the options variant, so offer exactly the types that have one.
		for i := range fields {
			if fields[i].Key == "type" && fields[i].Type == "string" {
				fields[i].Enum = OptionTypeNames(kind)
			}
		}
	}
	return SchemaNode{Type: "section", Fields: fields}
}

// variantNode builds the `options` schema for kind: one section per type, chosen by the sibling
// `type` key.
func (b schemaBuilder) variantNode(kind string) SchemaNode {
	variants := map[string]SchemaNode{}
	for name, t := range optionTypes[kind] {
		variants[name] = b.structNode(t, t.Name(), reflect.Value{})
	}
	return SchemaNode{Type: "variant", Discriminator: "type", Variants: variants}
}

// typeNode builds the schema for a field, map value or list element of type t. helpKey is the
// owner and field of the setting, used when t is an anonymous struct.
func (b schemaBuilder) typeNode(t reflect.Type, helpKey string, v reflect.Value) SchemaNode {
	if t == durationType {
		return SchemaNode{Type: "duration", Default: defaultOf(v)}
	}
	switch t.Kind() {
	case reflect.Pointer:
		if v.IsValid() {
			v = v.Elem() // a nil pointer gives an invalid Value: no defaults below it
		}
		return b.typeNode(t.Elem(), helpKey, v)
	case reflect.Struct:
		return b.structNode(t, helpKey, v)
	case reflect.Interface:
		return SchemaNode{Type: "any"}
	case reflect.Map:
		if t.Elem().Kind() == reflect.Interface {
			// A map[string]any is free-form: the UI edits it as a whole, not key by key.
			return SchemaNode{Type: "any"}
		}
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("config schema: %s has a map key type %s, want string", helpKey, t.Key()))
		}
		item := b.typeNode(t.Elem(), helpKey, itemValue(v))
		return SchemaNode{Type: "map", Item: &item}
	case reflect.Slice:
		item := b.typeNode(t.Elem(), helpKey, reflect.Value{})
		return SchemaNode{Type: "list", Item: &item}
	case reflect.String:
		return SchemaNode{Type: "string", Default: defaultOf(v)}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return SchemaNode{Type: "int", Default: defaultOf(v)}
	case reflect.Float32, reflect.Float64:
		return SchemaNode{Type: "float", Default: defaultOf(v)}
	case reflect.Bool:
		return SchemaNode{Type: "bool", Default: defaultOf(v)}
	}
	panic(fmt.Sprintf("config schema: %s has unsupported type %s", helpKey, t))
}

// leadWithYAMLKey rewrites help text that opens with the Go field name, such as "AllowedHosts are
// extra ...", so that it opens with the setting as the user sees it in the file: "Allowed hosts are
// extra ...". The key is humanized: underscores become spaces and the first letter is capitalised.
func leadWithYAMLKey(help, goName, key string) string {
	rest, ok := strings.CutPrefix(help, goName+" ")
	if !ok {
		return help
	}
	label := strings.ReplaceAll(key, "_", " ")
	return strings.ToUpper(label[:1]) + label[1:] + " " + rest
}

// hasOption reports whether a yaml tag's options (the part after the first comma) include opt.
func hasOption(opts, opt string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == opt {
			return true
		}
	}
	return false
}

// parseHelp maps "Type.Field" to the field's doc comment, or its trailing line comment when it
// has no doc comment, as one paragraph. Anonymous struct fields are keyed as "Type.Field.Sub".
func parseHelp(filename, src string) map[string]string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		panic("config schema: parse " + filename + ": " + err.Error())
	}
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				collectHelp(ts.Name.Name, st, out)
			}
		}
	}
	return out
}

func collectHelp(prefix string, st *ast.StructType, out map[string]string) {
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // an embedded struct has no name of its own to document
		}
		text := fieldText(field)
		for _, name := range field.Names {
			key := prefix + "." + name.Name
			if text != "" {
				out[key] = text
			}
			if inner, ok := field.Type.(*ast.StructType); ok {
				collectHelp(key, inner, out)
			}
		}
	}
}

// fieldText returns a field's doc comment, else its trailing comment, collapsed to one paragraph.
func fieldText(field *ast.Field) string {
	group := field.Doc
	if group == nil {
		group = field.Comment
	}
	if group == nil {
		return ""
	}
	return strings.Join(strings.Fields(group.Text()), " ")
}
