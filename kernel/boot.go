package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// principalQuery is the access contract of the module owning principals (SPEC §8); it is not a view.
const principalQuery = "Principal"

type pgType struct {
	name   string
	kind   byte
	elem   uint32
	labels []string
}

type catCol struct {
	name               string
	typ                uint32
	notNull, generated bool
	hasDefault         bool
	desc, def          string
}

type relation struct {
	kind byte
	desc string
	cols []catCol
}

type booter struct {
	*Kernel
	conn     *pgxpool.Conn
	rels     map[string]*relation
	attrs    map[[2]uint32]catCol // (rel oid, attnum)
	problems []string
}

func (b *booter) fail(format string, args ...any) {
	b.problems = append(b.problems, fmt.Sprintf(format, args...))
}

// Boot binds the static registry to the database: catalog, prepared queries, derived views and commands.
// The error lists every violation found.
func Boot(ctx context.Context, pool *pgxpool.Pool, s *Static) (*Kernel, error) {
	k := &Kernel{Static: s, Pool: pool, tables: map[string]*Table{}, views: map[string]*View{}, commands: map[string]*Command{},
		types: map[uint32]pgType{}, refs: map[string]string{},
		Meta: Meta{Tables: []*Table{}, Views: []*View{}, Commands: []*Command{}, Enums: map[string][]string{}}}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	b := &booter{Kernel: k, conn: conn, rels: map[string]*relation{}, attrs: map[[2]uint32]catCol{}}
	if err := b.catalog(ctx); err != nil {
		return nil, err
	}
	b.bootTables()
	b.bootViews(ctx)
	b.bootCommands(ctx)
	b.bootJobs(ctx)
	b.meta()
	if len(b.problems) > 0 {
		return k, errors.New(strings.Join(b.problems, "\n"))
	}
	return k, nil
}

func (b *booter) catalog(ctx context.Context) error {
	rows, err := b.conn.Query(ctx, `
select t.oid, t.typname, t.typtype, t.typelem, t.typnamespace = 'public'::regnamespace,
       coalesce((select array_agg(e.enumlabel order by e.enumsortorder) from pg_enum e where e.enumtypid = t.oid), '{}')
from pg_type t`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var oid, elem uint32
		var name, kind string
		var public bool
		var labels []string
		if err := rows.Scan(&oid, &name, &kind, &elem, &public, &labels); err != nil {
			return err
		}
		b.types[oid] = pgType{name, kind[0], elem, labels}
		if kind == "e" && public {
			b.Meta.Enums[name] = labels
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = b.conn.Query(ctx, `
select c.oid, c.relname, c.relkind, coalesce(obj_description(c.oid, 'pg_class'), ''), a.attnum, a.attname, a.atttypid,
       a.attnotnull, a.attidentity <> '' or a.attgenerated <> '', a.atthasdef, coalesce(col_description(c.oid, a.attnum), ''), coalesce(pg_get_expr(d.adbin, d.adrelid), '')
from pg_class c join pg_attribute a on a.attrelid = c.oid left join pg_attrdef d on d.adrelid = c.oid and d.adnum = a.attnum
where c.relnamespace = 'public'::regnamespace and c.relkind in ('r', 'p', 'v') and a.attnum > 0 and not a.attisdropped
order by c.relname, a.attnum`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var oid, typ uint32
		var rel, kind, rdesc string
		var attnum int16
		var c catCol
		if err := rows.Scan(&oid, &rel, &kind, &rdesc, &attnum, &c.name, &typ, &c.notNull, &c.generated, &c.hasDefault, &c.desc, &c.def); err != nil {
			return err
		}
		c.typ = typ
		r := b.rels[rel]
		if r == nil {
			r = &relation{kind: kind[0], desc: rdesc}
			b.rels[rel] = r
		}
		r.cols = append(r.cols, c)
		b.attrs[[2]uint32{oid, uint32(attnum)}] = c
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = b.conn.Query(ctx, `
select a.attname, f.relname
from pg_constraint c
join pg_attribute a on a.attrelid = c.conrelid and a.attnum = c.conkey[1]
join pg_class f on f.oid = c.confrelid
where c.contype = 'f' and c.connamespace = 'public'::regnamespace and cardinality(c.conkey) = 1
group by 1, 2 order by 1, 2`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var col, ref string
		if err := rows.Scan(&col, &ref); err != nil {
			return err
		}
		if prev, ok := b.refs[col]; ok {
			b.fail("FK column name %s is ambiguous: it references %s and %s", col, prev, ref)
			continue
		}
		b.refs[col] = ref
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for col, ref := range b.Refs {
		switch prev, ok := b.refs[col]; {
		case b.rels[ref] == nil:
			b.fail("Ref(%s, %s): %s is not a table", col, ref, ref)
		case ok && prev != ref:
			b.fail("Ref(%s, %s): %s is a foreign key to %s", col, ref, col, prev)
		default:
			b.refs[col] = ref
		}
	}
	return nil
}

// formats is the format of a column by the last word of its name (SPEC §4); dates and instants go by their type.
var formats = map[string]string{"min": "min", "tod": "tod", "pct": "pct", "coef": "coef",
	"amount": "money", "rate": "money", "cost": "money", "price": "money"}

func (k *Kernel) col(name string, oid uint32, nullable bool, desc string) Col {
	t := k.types[oid]
	c := Col{Name: name, Type: t.name, Format: formats[name[strings.LastIndex(name, "_")+1:]], Nullable: nullable, Description: desc,
		Ref: k.refs[name]}
	if name == "id" {
		c.Ref = ""
	}
	if t.kind == 'e' {
		c.Enum = t.name
	} else if e, ok := k.types[t.elem]; ok && e.kind == 'e' {
		c.Enum = e.name
	}
	return c
}

func (b *booter) bootTables() {
	for _, name := range b.tableSeq {
		d := b.Tables[name]
		r := b.rels[name]
		if r == nil || r.kind == 'v' {
			b.fail("table %s: declared but not a table of schema public", name)
			continue
		}
		t := &Table{Name: name, Read: d.Read, Write: d.Write, Label: d.Label, CRUD: !d.NoCRUD, Under: d.Under,
			Anchor: d.Anchor, ChildFK: d.ChildFK, Description: r.desc, Actions: map[string][]string{}, Conditions: []string{},
			decl: d, source: name}
		for _, c := range r.cols {
			t.Columns = append(t.Columns, TableCol{Col: b.col(c.name, c.typ, !c.notNull, c.desc),
				Guarded: slices.Contains(d.Guarded, c.name), HasDefault: c.hasDefault || c.generated, generated: c.generated, def: c.def})
		}
		if t.Label == "" && t.column("name") != nil {
			t.Label = "name"
		}
		for _, c := range append(append([]string{t.Label, d.ChildFK}, d.Guarded...), anchorCol(d.Anchor)) {
			if c != "" && t.column(c) == nil {
				b.fail("table %s: column %s does not exist", name, c)
			}
		}
		if p := b.rels[name+"_p"]; p != nil && p.kind == 'v' {
			t.source = name + "_p"
			for _, c := range p.cols {
				if b.types[c.typ].name == "bool" && t.column(c.name) == nil {
					t.Conditions = append(t.Conditions, c.name)
				}
			}
		}
		b.tables[name] = t
		b.Meta.Tables = append(b.Meta.Tables, t)
	}
	for _, t := range b.Meta.Tables {
		t.root = t
		if t.decl.ChildOf != "" {
			r := b.tables[t.decl.ChildOf]
			if r == nil || r.decl.ChildOf != "" {
				b.fail("table %s: ChildOf %s, which is not a declared root", t.Name, t.decl.ChildOf)
				continue
			}
			if t.decl.ChildFK == "" {
				b.fail("table %s: ChildOf without FK column", t.Name)
			}
			t.root = r
			r.children = append(r.children, t)
		}
		t.Root = t.root.Name
		if u := b.tables[t.Under]; t.Under != "" && (u == nil || u.decl.ChildOf != "") {
			b.fail("table %s: Under %s, which is not a declared root", t.Name, t.Under)
		}
	}
	for _, t := range b.Meta.Tables {
		acts := b.Actions[t.Name]
		if t.root != t {
			if len(acts) > 0 {
				b.fail("table %s: actions are declared on its root %s", t.Name, t.Root)
			}
			continue
		}
		for a, conds := range acts {
			for _, c := range conds {
				if !slices.Contains(t.Conditions, c) {
					b.fail("action %s.%s: condition %s is not a boolean column of %s_p", t.Name, a, c, t.Name)
				}
			}
			t.Actions[a] = conds
		}
		for _, a := range []string{"editable", "deletable"} {
			if _, ok := t.Actions[a]; !ok {
				t.Actions[a] = []string{}
			}
		}
	}
	for table := range b.Actions {
		if b.tables[table] == nil {
			b.fail("actions declared on unknown table %s", table)
		}
	}
}

func anchorCol(a *AnchorDecl) string {
	if a == nil {
		return ""
	}
	return a.Column
}

func (t *Table) column(name string) *TableCol {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

// reachFilter is the SQL restricting rows of t (alias r) to the anchor of its root, $n being the reach.
func (t *Table) reachFilter(n int) string {
	a := t.root.Anchor
	switch {
	case a == nil:
		return "true"
	case t.root == t:
		return fmt.Sprintf("r.%s = any($%d)", ident(a.Column), n)
	default:
		return fmt.Sprintf("r.%s in (select id from %s where %s = any($%d))", ident(t.ChildFK), ident(t.Root), ident(a.Column), n)
	}
}

func (b *booter) describe(ctx context.Context, sql string) (*pgconn.StatementDescription, error) {
	return b.conn.Conn().PgConn().Prepare(ctx, "", sql, nil)
}

func (b *booter) bootViews(ctx context.Context) {
	for _, t := range b.Meta.Tables {
		var params []string
		where := ""
		if t.root.Anchor != nil {
			params, where = []string{"reach"}, " where "+t.reachFilter(1)
		}
		b.view(ctx, &View{Name: t.Name, Needs: t.Read, Anchor: t.root.Anchor, Description: t.Description,
			sql: "select * from " + ident(t.source) + " r" + where, params: params}, nil, t)
		if b.Queries[pascal(t.Name)+"Lookup"] == nil {
			label := "r.id::text"
			if t.Label != "" {
				label = "r." + ident(t.Label) + "::text"
			}
			b.view(ctx, &View{Name: t.Name + ".lookup", Needs: t.Read, Anchor: t.root.Anchor,
				sql: "select r.id, " + label + " as label from " + ident(t.Name) + " r" + where, params: params}, []bool{true, false}, nil)
		}
	}
	for _, name := range sortedKeys(b.Queries) {
		q := b.Queries[name]
		if name == principalQuery {
			b.principal = q
			if _, err := b.describe(ctx, q.Text); err != nil {
				b.fail("query %s: %v", name, err)
			}
			continue
		}
		if !q.isView() {
			continue
		}
		v := &View{Name: viewName(name), Needs: q.Needs, Anchor: q.Anchor, Description: q.Description, sql: q.Text, params: q.Params,
			history: name == "History"}
		if b.views[v.Name] != nil {
			b.fail("query %s: view name %s already taken by a generic view", name, v.Name)
			continue
		}
		b.view(ctx, v, q.NotNull, nil)
	}
	if b.principal == nil {
		b.fail("query %s(@subjects) is missing from the manifest", principalQuery)
	}
}

// view describes v and derives its params, columns and roots. Nullability comes from notNull, or from table t.
func (b *booter) view(ctx context.Context, v *View, notNull []bool, t *Table) {
	sd, err := b.describe(ctx, v.sql)
	if err != nil {
		b.fail("view %s: %v", v.Name, err)
		return
	}
	v.Params, v.Columns, v.Roots = []Col{}, []Col{}, []string{}
	for i, oid := range sd.ParamOIDs {
		if !kernelParams[v.params[i]] {
			v.Params = append(v.Params, b.col(v.params[i], oid, true, ""))
		}
	}
	for i, f := range sd.Fields {
		nullable := !(i < len(notNull) && notNull[i])
		if t != nil {
			tc := t.column(f.Name)
			nullable = tc == nil || tc.Nullable
		}
		v.Columns = append(v.Columns, b.col(f.Name, f.DataTypeOID, nullable, b.attrs[[2]uint32{f.TableOID, uint32(f.TableAttributeNumber)}].desc))
	}
	res, err := b.conn.Conn().PgConn().Exec(ctx, "explain (generic_plan, format json) "+v.sql).ReadAll()
	if err != nil {
		b.fail("view %s: explain: %v", v.Name, err)
		return
	}
	plan := res[0].Rows[0][0]
	var tree any
	if err := json.Unmarshal(plan, &tree); err != nil {
		b.fail("view %s: explain: %v", v.Name, err)
		return
	}
	for _, rel := range relations(tree) {
		if t := b.tables[rel]; t != nil && !slices.Contains(v.Roots, t.Root) {
			v.Roots = append(v.Roots, t.Root)
		}
	}
	slices.Sort(v.Roots)
	b.views[v.Name] = v
	b.Meta.Views = append(b.Meta.Views, v)
}

func relations(n any) []string {
	var out []string
	switch n := n.(type) {
	case map[string]any:
		if r, ok := n["Relation Name"].(string); ok {
			out = append(out, r)
		}
		for _, c := range n {
			out = append(out, relations(c)...)
		}
	case []any:
		for _, c := range n {
			out = append(out, relations(c)...)
		}
	}
	return out
}

func pascal(s string) string {
	var b strings.Builder
	for p := range strings.SplitSeq(s, "_") {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

// effect is one :exec statement of a command, run wrapped with its event insert.
type effect struct {
	sql    string
	params []string
	table  *Table
}

var dmlTarget = regexp.MustCompile(`(?is)^\s*(?:insert\s+into|update|delete\s+from)\s+(?:only\s+)?(?:public\.)?"?([a-z_][a-z0-9_]*)"?`)

func (b *booter) bootCommands(ctx context.Context) {
	for _, t := range b.Meta.Tables {
		if !t.CRUD {
			continue
		}
		writable := []Col{}
		for _, c := range t.Columns {
			if c.Name != "id" && !c.generated && !c.Guarded {
				c.required = !c.Nullable && !c.HasDefault
				writable = append(writable, c.Col)
			}
		}
		optional := make([]Col, len(writable))
		for i, c := range writable {
			c.required = false
			optional[i] = c
		}
		child := t.root != t
		add := func(kind, when, ensure string, input []Col) {
			if child {
				when, ensure = "editable", "editable"
			}
			b.addCommand(&Command{Name: t.Name + "." + kind, Table: t.Name, Kind: kind, Input: input, When: when, Ensure: ensure,
				Needs: t.Write, table: t, ensureT: t.root, ensureA: ensure})
		}
		add("create", "", "editable", writable)
		if !t.decl.AppendOnly {
			add("update", "editable", "editable", optional)
			add("delete", "deletable", "", []Col{})
		}
	}
	for _, d := range b.Static.Commands {
		c := &Command{Name: d.Name, Kind: "custom", Rootless: d.Rootless, When: d.When, Ensure: d.Ensure, Reach: d.Reach, Input: []Col{}}
		if d.On != "" {
			c.table = b.tables[d.On]
			if c.table == nil {
				b.fail("command %s: On(%s) is not a declared table", d.Name, d.On)
				continue
			}
			c.Table = d.On
			c.Needs = c.table.Write
		}
		if d.Needs != nil {
			c.Needs = *d.Needs
		}
		if c.table == nil && (c.When != "" || c.Ensure != "" || !c.Rootless && d.Run == nil) {
			b.fail("command %s: When, Ensure and ids need On(table)", d.Name)
			continue
		}
		if c.When != "" && c.table.root.Actions[c.When] == nil {
			b.fail("command %s: When(%s) is not an action of %s", d.Name, c.When, c.table.Root)
		}
		if c.Ensure != "" {
			c.ensureT, c.ensureA = c.table.root, c.Ensure
			if tn, a, ok := strings.Cut(c.Ensure, "."); ok {
				if t := b.tables[tn]; t != nil {
					c.ensureT = t.root
				} else {
					c.ensureT = nil
				}
				c.ensureA = a
			}
			if c.ensureT == nil || c.ensureT.Actions[c.ensureA] == nil {
				b.fail("command %s: Ensure(%s) is not an action", d.Name, c.Ensure)
			}
		}
		if d.Reach != "" && (c.table == nil || c.table.root.Anchor == nil) {
			b.fail("command %s: Reach(%s) on a table without anchor", d.Name, d.Reach)
		}
		if d.Run != nil {
			c.Kind, c.run = "run", d.Run
			v := b.views[d.Run.Candidates]
			if v == nil {
				b.fail("run %s: candidates view %s does not exist", d.Name, d.Run.Candidates)
			} else {
				c.Input = v.Params
			}
		}
		var desc []string
		for _, name := range d.Effects {
			q := b.Queries[name]
			if q == nil || q.isView() {
				b.fail("command %s: effect %s is not an :exec query of the manifest", d.Name, name)
				continue
			}
			if q.Description != "" {
				desc = append(desc, q.Description)
			}
			e := b.effect(ctx, q, c)
			if e == nil {
				continue
			}
			locks := slices.ContainsFunc(c.effects, func(x *effect) bool { return x.table == nil })
			switch {
			case e.table == nil && len(c.effects) > 0 && c.effects[len(c.effects)-1].table != nil:
				b.fail("command %s: lock statement %s after a write", d.Name, name)
			case slices.Contains(e.params, "locked") && (e.table == nil || !locks):
				b.fail("command %s: %s uses @locked without a lock statement before it", d.Name, name)
			}
			c.effects = append(c.effects, e)
		}
		c.Description = strings.Join(desc, " ")
		b.addCommand(c)
	}
	for cmd := range b.Reactions {
		if b.commands[cmd] == nil && !slices.ContainsFunc(b.Static.Commands, func(d *CommandDecl) bool { return d.Name == cmd }) {
			b.fail("reaction on unknown command %s", cmd)
		}
	}
}

func (b *booter) addCommand(c *Command) {
	if b.commands[c.Name] != nil {
		b.fail("command %s declared twice", c.Name)
		return
	}
	b.commands[c.Name] = c
	b.Meta.Commands = append(b.Meta.Commands, c)
}

func (b *booter) effect(ctx context.Context, q *Query, c *Command) *effect {
	if strings.Contains(strings.ToLower(q.Text), "returning") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(q.Text)), "with") {
		b.fail("effect %s: must be a simple DML statement (no leading with, no returning)", q.Name)
		return nil
	}
	var t *Table // nil: a lock statement (select … for update), run as is
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(q.Text)), "select") {
		m := dmlTarget.FindStringSubmatch(q.Text)
		if m == nil {
			b.fail("effect %s: not a select, insert, update or delete", q.Name)
			return nil
		}
		if t = b.tables[m[1]]; t == nil {
			b.fail("effect %s: table %s is not declared", q.Name, m[1])
			return nil
		}
	}
	sd, err := b.describe(ctx, q.Text)
	if err != nil {
		b.fail("effect %s: %v", q.Name, err)
		return nil
	}
	if t == nil && (len(sd.Fields) != 1 || b.types[sd.Fields[0].DataTypeOID].name != "int8") {
		b.fail("effect %s: a lock statement returns the ids it locks, one bigint column", q.Name)
		return nil
	}
	for i, oid := range sd.ParamOIDs {
		name := q.Params[i]
		if kernelParams[name] || slices.ContainsFunc(c.Input, func(x Col) bool { return x.Name == name }) {
			continue
		}
		col := b.col(name, oid, q.ParamNull[i], "")
		col.required = !q.ParamNull[i]
		c.Input = append(c.Input, col)
	}
	return &effect{sql: q.Text, params: q.Params, table: t}
}

func (b *booter) meta() {
	b.Meta.Functions = nonNil(b.Meta.Enums["fn"])
	all := slices.Clone(b.Screens)
	for _, t := range b.Meta.Tables {
		if t.decl.Group != "" {
			all = append(all, tableScreen(t))
		}
	}
	b.Meta.Screens = screens(all)
	b.Meta.Groups = []string{}
	ids := map[string]bool{}
	for _, s := range b.Meta.Screens {
		if ids[s.ID] {
			b.fail("screen %s declared twice", s.ID)
		}
		ids[s.ID] = true
		if s.Group != "" && !slices.Contains(b.Meta.Groups, s.Group) {
			b.Meta.Groups = append(b.Meta.Groups, s.Group)
		}
	}
	slices.SortFunc(b.Meta.Views, func(x, y *View) int { return strings.Compare(x.Name, y.Name) })
	slices.SortFunc(b.Meta.Commands, func(x, y *Command) int { return strings.Compare(x.Name, y.Name) })
	for _, v := range b.Meta.Views {
		b.viewSchemas(v)
	}
	for _, c := range b.Meta.Commands {
		b.commandSchema(c)
	}
}

// tableScreen is the generated screen of a table declared with Group (SPEC §19): its list view, its CRUD and a
// tab per child table, read by its foreign key.
func tableScreen(t *Table) Screen {
	s := Screen{ID: t.Name, Group: t.decl.Group, Views: []string{t.Name}, Table: t.Name}
	for _, c := range t.children {
		s.Children = append(s.Children, Screen{ID: c.Name, Views: []string{c.Name}, Table: c.Name, Link: map[string]string{c.ChildFK: "id"}})
	}
	return s
}

// screens copies ss with the lists and maps of the contract never null.
func screens(ss []Screen) []Screen {
	out := make([]Screen, len(ss))
	for i, s := range ss {
		s.Views, s.Commands, s.Children = nonNil(s.Views), nonNil(s.Commands), screens(s.Children)
		if s.Defaults == nil {
			s.Defaults = map[string]string{}
		}
		out[i] = s
	}
	return out
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ident quotes an SQL identifier.
func ident(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
