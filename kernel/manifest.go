package kernel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Query is a query of the sqlc manifest with its annotations.
type Query struct {
	Name        string // sqlc name
	Text        string
	Cmd         string
	Params      []string // param names, index = $n - 1
	ParamNull   []bool   // per param: sqlc.narg
	NotNull     []bool   // per output column, from sqlc
	Anchor      *AnchorDecl
	AnchorNone  bool
	Needs       string
	Description string
	File        string
}

type manifest struct {
	Queries []struct {
		Text   string `json:"text"`
		Name   string `json:"name"`
		Cmd    string `json:"cmd"`
		Params []struct {
			Number int `json:"number"`
			Column struct {
				Name    string `json:"name"`
				NotNull bool   `json:"not_null"`
			} `json:"column"`
		} `json:"params"`
		Columns []struct {
			NotNull bool `json:"not_null"`
		} `json:"columns"`
		Comments []string `json:"comments"`
		Filename string   `json:"filename"`
	} `json:"queries"`
}

// kernel params are bound by the kernel, never by the caller.
var kernelParams = map[string]bool{"reach": true, "self": true, "ids": true}

func parseManifest(data []byte) (map[string]*Query, []string, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("manifest: %w", err)
	}
	qs := map[string]*Query{}
	var problems []string
	for _, mq := range m.Queries {
		q := &Query{Name: mq.Name, Text: mq.Text, Cmd: mq.Cmd, File: mq.Filename}
		for _, p := range mq.Params {
			for len(q.Params) < p.Number {
				q.Params = append(q.Params, "")
				q.ParamNull = append(q.ParamNull, true)
			}
			q.Params[p.Number-1] = p.Column.Name
			q.ParamNull[p.Number-1] = !p.Column.NotNull
		}
		for _, c := range mq.Columns {
			q.NotNull = append(q.NotNull, c.NotNull)
		}
		var desc []string
		for _, line := range mq.Comments {
			f := strings.Fields(line)
			switch {
			case len(f) == 2 && f[0] == "@anchor" && f[1] == "none":
				q.AnchorNone = true
			case len(f) == 3 && f[0] == "@anchor":
				q.Anchor = &AnchorDecl{f[1], f[2]}
			case len(f) == 2 && f[0] == "@needs":
				q.Needs = f[1]
			case len(f) > 0 && strings.HasPrefix(f[0], "@"):
				problems = append(problems, fmt.Sprintf("query %s: malformed annotation %q", q.Name, strings.TrimSpace(line)))
			default:
				desc = append(desc, strings.TrimSpace(line))
			}
		}
		q.Description = strings.TrimSpace(strings.Join(desc, " "))
		if qs[q.Name] != nil {
			problems = append(problems, fmt.Sprintf("query %s declared twice", q.Name))
		}
		qs[q.Name] = q
	}
	return qs, problems, nil
}

func (q *Query) isView() bool { return q.Cmd == ":many" || q.Cmd == ":one" }

// snake converts a sqlc name to a registry name: ActivityGrid → activity_grid.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

var fnEnum = regexp.MustCompile(`(?is)create\s+type\s+(?:public\.)?fn\s+as\s+enum\s*\(([^)]*)\)`)

// schemaFunctions reads the capability functions: the values of enum fn in the module schemas.
func schemaFunctions(mods []Module) []string {
	var fns []string
	for _, m := range mods {
		for _, g := range fnEnum.FindAllStringSubmatch(m.Schema, -1) {
			for v := range strings.SplitSeq(g[1], ",") {
				fns = append(fns, strings.Trim(strings.TrimSpace(v), "'"))
			}
		}
	}
	return fns
}

// Static is the registry known without a database: declarations and manifest.
type Static struct {
	*Registry
	Modules   []Module
	Queries   map[string]*Query
	Functions []string
	problems  []string
}

// Load registers Core then mods and parses the manifest.
func Load(manifestJSON []byte, mods ...Module) (*Static, error) {
	mods = append([]Module{Core}, mods...)
	r := newRegistry()
	for _, m := range mods {
		if m.Register != nil {
			m.Register(r)
		}
	}
	qs, problems, err := parseManifest(manifestJSON)
	if err != nil {
		return nil, err
	}
	return &Static{Registry: r, Modules: mods, Queries: qs, Functions: schemaFunctions(mods), problems: problems}, nil
}

// effectQueries are the :exec queries referenced by commands.
func (s *Static) effectQueries() map[string]bool {
	used := map[string]bool{}
	for _, c := range s.Commands {
		for _, e := range c.Effects {
			used[e] = true
		}
	}
	return used
}

// CheckStatic lists the violations visible without a database. labels is labels/fr.json, or nil.
func (s *Static) CheckStatic(labels map[string]string) []string {
	p := slices.Clone(s.problems)
	fn := func(where, f string) {
		if f != "" && !slices.Contains(s.Functions, f) {
			p = append(p, fmt.Sprintf("%s: function %q is not a value of enum fn", where, f))
		}
	}
	used := s.effectQueries()
	for _, name := range sortedKeys(s.Queries) {
		q := s.Queries[name]
		if !q.isView() {
			if !used[name] {
				p = append(p, fmt.Sprintf("query %s: :exec query referenced by no command", name))
			}
			continue
		}
		if name == principalQuery {
			continue
		}
		switch {
		case q.Anchor == nil && !q.AnchorNone:
			p = append(p, fmt.Sprintf("query %s: missing @anchor (@anchor <column> <reach> or @anchor none)", name))
		case q.Anchor != nil && !slices.Contains(q.Params, "reach"):
			p = append(p, fmt.Sprintf("query %s: anchored on %s but has no @reach param", name, q.Anchor.Column))
		}
		fn("query "+name, q.Needs)
	}
	for _, c := range s.Commands {
		for _, e := range c.Effects {
			if q := s.Queries[e]; q == nil {
				p = append(p, fmt.Sprintf("command %s: effect query %s is not in the manifest", c.Name, e))
			} else if q.isView() {
				p = append(p, fmt.Sprintf("command %s: effect query %s is %s, not :exec", c.Name, e, q.Cmd))
			}
		}
		if c.Needs != nil {
			fn("command "+c.Name, *c.Needs)
		}
		if c.On == "" && !c.Rootless && c.Run == nil {
			p = append(p, fmt.Sprintf("command %s: needs On(table) or Rootless()", c.Name))
		}
		if c.On != "" && s.Tables[c.On] == nil {
			p = append(p, fmt.Sprintf("command %s: table %s is not declared", c.Name, c.On))
		}
	}
	for _, name := range s.tableSeq {
		t := s.Tables[name]
		fn("table "+name, t.Read)
		fn("table "+name, t.Write)
	}
	if labels != nil {
		for _, k := range s.labelKeys() {
			if labels[k] == "" {
				p = append(p, fmt.Sprintf("labels/fr.json: missing %q", k))
			}
		}
	}
	return p
}

// labelKeys are the registry keys that need a label.
func (s *Static) labelKeys() []string {
	keys := map[string]bool{}
	for _, name := range s.tableSeq {
		t := s.Tables[name]
		keys["t."+name] = true
		keys["v."+name] = true
		if !t.NoCRUD {
			keys["cmd."+name+".create"] = true
			if !t.AppendOnly {
				keys["cmd."+name+".update"] = true
				keys["cmd."+name+".delete"] = true
			}
		}
		if t.Group != "" {
			keys["g."+t.Group] = true
		}
	}
	for name, q := range s.Queries {
		if q.isView() && name != principalQuery {
			keys["v."+viewName(name)] = true
		}
	}
	for _, c := range s.Commands {
		keys["cmd."+c.Name] = true
	}
	for _, acts := range s.Actions {
		for _, conds := range acts {
			for _, c := range conds {
				keys["cond."+c] = true
			}
		}
	}
	for _, f := range s.Functions {
		keys["fn."+f] = true
	}
	var walk func([]Screen)
	walk = func(ss []Screen) {
		for _, sc := range ss {
			keys["s."+sc.ID] = true
			if sc.Group != "" {
				keys["g."+sc.Group] = true
			}
			walk(sc.Children)
		}
	}
	walk(s.Screens)
	return sortedKeys(keys)
}

// viewName is the registry name of a view query; <T>Lookup replaces t.lookup.
func viewName(sqlcName string) string {
	n := snake(sqlcName)
	if t, ok := strings.CutSuffix(n, "_lookup"); ok {
		return t + ".lookup"
	}
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}
