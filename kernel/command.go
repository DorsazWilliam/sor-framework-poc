package kernel

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Call runs a command (SPEC §7): lock and read (1 round trip), effects and re-read (1), commit (1).
func (k *Kernel) Call(ctx context.Context, p *Principal, name string, body []byte) (*Result, error) {
	c := k.commands[name]
	if c == nil {
		return nil, problem(404, "not_found", "no command "+name)
	}
	if pr := p.can(c.Needs); pr != nil {
		return nil, pr
	}
	in := map[string]any{}
	if pr := decode(c, body, &in); pr != nil {
		return nil, pr
	}
	conn, err := k.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	x := &call{k: k, c: c, p: p, in: in, conn: conn, id: uuid()}
	res, err := x.run(ctx)
	if err != nil {
		conn.Exec(context.WithoutCancel(ctx), "rollback")
		return nil, pgProblem(err)
	}
	return res, nil
}

type call struct {
	k    *Kernel
	c    *Command
	p    *Principal
	in   map[string]any
	conn *pgxpool.Conn
	id   string
	ids  []int64
}

type stmt struct {
	sql   string
	args  []any
	table *Table // nil: run as is
}

func (x *call) reach() string {
	r := x.c.Reach
	if r == "" && x.c.table != nil {
		r = reachName(x.c.table.root.Anchor)
	}
	return ints(x.p.Reach[r])
}

func (x *call) run(ctx context.Context) (*Result, error) {
	c := x.c
	if c.Kind == "run" {
		return x.startRun(ctx)
	}
	for _, v := range asSlice(x.in["ids"]) {
		f, _ := v.(float64)
		x.ids = append(x.ids, int64(f))
	}
	slices.Sort(x.ids)
	x.ids = slices.Compact(x.ids)
	var root *Table
	if c.table != nil {
		root = c.table.root
	}

	// Round trip 2: begin, locks, projection of the roots.
	b := &pgx.Batch{}
	b.Queue("begin")
	if root != nil && root.Under != "" {
		b.Queue("select id from " + ident(root.Under) + " order by id for share")
	}
	var rootExpr string
	var rootArg any
	child := root != nil && c.table != root
	switch {
	case root == nil || c.Rootless || c.Kind == "create" && !child:
	case !child:
		rootExpr, rootArg = "$1::bigint[]", ints(x.ids)
	case c.Kind == "create":
		// the root is the parent the insert will reference, the column default when omitted
		def := "null"
		if d := c.table.column(c.table.ChildFK).def; d != "" {
			def = d
		}
		rootExpr, rootArg = "array[coalesce($1::bigint, "+def+")]", pgValue(x.in[c.table.ChildFK], "int8")
	default:
		rootExpr, rootArg = fmt.Sprintf("array(select %s from %s where id = any($1::bigint[]))", ident(c.table.ChildFK), ident(c.table.Name)), ints(x.ids)
		b.Queue("select count(*) from "+ident(c.table.Name)+" where id = any($1::bigint[])", rootArg)
	}
	counted := rootExpr != "" && child && c.Kind != "create"
	plain := len(b.QueuedQueries)
	if counted {
		plain--
	}
	if rootExpr != "" {
		b.Queue("select id from "+ident(root.Name)+" where id = any("+rootExpr+") order by id for update", rootArg)
		b.Queue("select id, to_jsonb(r) from "+ident(root.source)+" r where id = any("+rootExpr+")", rootArg)
	}
	br := x.conn.SendBatch(ctx, b)
	var before map[int64]json.RawMessage
	var found int
	err := func() error {
		for range plain {
			if _, err := br.Exec(); err != nil {
				return err
			}
		}
		if counted {
			if err := br.QueryRow().Scan(&found); err != nil {
				return err
			}
		}
		if rootExpr == "" {
			return nil
		}
		if _, err := br.Exec(); err != nil {
			return err
		}
		var err error
		before, err = projRows(br)
		return err
	}()
	if cerr := br.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if counted && found != len(x.ids) {
		return nil, problem(404, "not_found", "")
	}
	if rootExpr != "" {
		if pr := x.checkBefore(root, before); pr != nil {
			return nil, pr
		}
	}

	// Round trip 3: effects with their events, re-read of the touched roots.
	stmts, err := x.statements()
	if err != nil {
		return nil, err
	}
	b = &pgx.Batch{}
	for _, s := range stmts {
		if s.table == nil {
			b.Queue(s.sql, s.args...)
			continue
		}
		n := len(s.args)
		rootFK := "id"
		if s.table.root != s.table {
			rootFK = s.table.ChildFK
		}
		b.Queue(fmt.Sprintf(`with eff as (%s returning to_jsonb(old) as o, to_jsonb(new) as n)
insert into event (call, command, principal_id, for_id, tbl, row_id, root_table, root_id, changes)
select $%d::uuid, $%d::text, $%d::bigint, $%d::bigint, %s, (coalesce(n, o)->>'id')::bigint, %s, (coalesce(n, o)->>%s)::bigint, sor_diff(o, n)
from eff where o is distinct from n
returning tbl, row_id, root_table, root_id`, s.sql, n+1, n+2, n+3, n+4, literal(s.table.Name), literal(s.table.Root), literal(rootFK)),
			append(s.args, x.id, c.Name, strconv.FormatInt(x.p.ID, 10), optInt(x.p.forID()))...)
	}
	touched := func(t *Table) string {
		return "(select root_id from event where call = $1::uuid and root_table = " + literal(t.Name) + ")"
	}
	var reread []*Table
	for _, t := range []*Table{root, c.ensureT} {
		if t != nil && !slices.Contains(reread, t) {
			reread = append(reread, t)
			b.Queue("select id, to_jsonb(r) from "+ident(t.source)+" r where id in "+touched(t), x.id)
		}
	}
	var anchored []*Table
	for _, s := range stmts {
		if s.table != nil && s.table.root.Anchor != nil && !slices.Contains(anchored, s.table.root) {
			t := s.table.root
			anchored = append(anchored, t)
			a := ident(t.Anchor.Column)
			b.Queue(fmt.Sprintf(`select coalesce(array_agg(distinct a), '{}') from (
select r.%s::bigint a from %s r where r.id in %s
union all select (e.changes->%s->>0)::bigint from event e where e.call = $1::uuid and e.tbl = %s) x where a is not null`,
				a, ident(t.Name), touched(t), literal(t.Anchor.Column), literal(t.Name)), x.id)
		}
	}
	var rows []Row
	after := map[*Table]map[int64]json.RawMessage{}
	anchors := map[string][]int64{}
	if len(b.QueuedQueries) == 0 {
		b.Queue("select")
	}
	br = x.conn.SendBatch(ctx, b)
	err = func() error {
		for _, s := range stmts {
			if s.table == nil {
				if _, err := br.Exec(); err != nil {
					return err
				}
				continue
			}
			rs, err := br.Query()
			if err != nil {
				return err
			}
			for rs.Next() {
				var r Row
				if err := rs.Scan(&r.Table, &r.ID, &r.RootTable, &r.RootID); err != nil {
					return err
				}
				rows = append(rows, r)
			}
			if err := rs.Err(); err != nil {
				return err
			}
		}
		for _, t := range reread {
			m, err := projRows(br)
			if err != nil {
				return err
			}
			after[t] = m
		}
		for _, t := range anchored {
			var a []int64
			if err := br.QueryRow().Scan(&a); err != nil {
				return err
			}
			anchors[t.Name] = a
		}
		return nil
	}()
	if cerr := br.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if root != nil {
		if pr := x.checkReach(root, after[root]); pr != nil {
			return nil, pr
		}
	}
	if c.ensureT != nil {
		if pr := rejection(c.ensureA, c.ensureT.Actions[c.ensureA], after[c.ensureT]); pr != nil {
			return nil, pr
		}
	}

	// Round trip 4: what the hook queues, live notifications, commit.
	res := &Result{Changed: []Changed{}, IDs: []int64{}}
	if rootExpr != "" {
		res.IDs = sortedKeys64(before)
	}
	keys := map[string][]int64{}
	var order []string
	for _, r := range rows {
		i := slices.IndexFunc(res.Changed, func(ch Changed) bool { return ch.Table == r.Table })
		if i < 0 {
			res.Changed, i = append(res.Changed, Changed{Table: r.Table}), len(res.Changed)
		}
		res.Changed[i].IDs = append(res.Changed[i].IDs, r.ID)
		if root != nil && r.RootTable == root.Name {
			res.IDs = append(res.IDs, r.RootID)
		}
		if _, ok := keys[r.RootTable]; !ok {
			order = append(order, r.RootTable)
		}
		keys[r.RootTable] = append(keys[r.RootTable], r.RootID)
	}
	for i := range res.Changed {
		res.Changed[i].IDs = uniq(res.Changed[i].IDs)
	}
	res.IDs = uniq(res.IDs)
	b = &pgx.Batch{}
	if err := x.beforeCommit(ctx, b, res, rows); err != nil {
		return nil, err
	}
	for _, t := range order {
		b.Queue("select pg_notify('sor_live', $1)", livePayload(t, uniq(keys[t]), anchors[t], c.Name, x.id))
	}
	b.Queue("commit")
	if err := x.conn.SendBatch(ctx, b).Close(); err != nil {
		return nil, err
	}
	return res, nil
}

func (x *call) beforeCommit(ctx context.Context, b *pgx.Batch, res *Result, rows []Row) error {
	if x.k.BeforeCommit == nil {
		return nil
	}
	return x.k.BeforeCommit(ctx, b, &Commit{Command: x.c.Name, Call: x.id, Principal: x.p.ID, For: x.p.forID(),
		Subjects: x.p.Subjects, Roots: res.IDs, Rows: rows, Run: res.Run})
}

// checkBefore: every root exists and is in reach, then When holds.
func (x *call) checkBefore(root *Table, rows map[int64]json.RawMessage) *Problem {
	if x.c.table == root && x.c.Kind != "create" && len(rows) != len(x.ids) {
		return problem(404, "not_found", "")
	}
	if len(rows) == 0 {
		return problem(404, "not_found", "")
	}
	if pr := x.checkReach(root, rows); pr != nil {
		return pr
	}
	if x.c.When == "" {
		return nil
	}
	return rejection(x.c.When, root.Actions[x.c.When], rows)
}

func (x *call) checkReach(root *Table, rows map[int64]json.RawMessage) *Problem {
	if root.Anchor == nil {
		return nil
	}
	reach := x.p.Reach[x.c.Reach]
	if x.c.Reach == "" {
		reach = x.p.Reach[root.Anchor.Reach]
	}
	for _, raw := range rows {
		var row map[string]any
		json.Unmarshal(raw, &row)
		a, _ := row[root.Anchor.Column].(float64)
		if !slices.Contains(reach, int64(a)) {
			return problem(404, "not_found", "")
		}
	}
	return nil
}

func rejection(action string, conds []string, rows map[int64]json.RawMessage) *Problem {
	var rej []Rejected
	for _, id := range sortedKeys64(rows) {
		var row map[string]any
		json.Unmarshal(rows[id], &row)
		var f []string
		for _, c := range conds {
			if row[c] != true {
				f = append(f, c)
			}
		}
		if len(f) > 0 {
			rej = append(rej, Rejected{ID: id, Conditions: f, Row: rows[id]})
		}
	}
	if rej == nil {
		return nil
	}
	p := problem(409, "rejected", action)
	p.Action, p.Rejected = action, rej
	return p
}

// statements are the effects of the call, in order.
func (x *call) statements() ([]stmt, error) {
	c, t := x.c, x.c.table
	switch c.Kind {
	case "create", "update":
		var cols, vals []string
		var args []any
		for _, col := range c.Input {
			if v, ok := x.in[col.Name]; ok {
				args = append(args, pgValue(v, col.Type))
				cols, vals = append(cols, ident(col.Name)), append(vals, fmt.Sprintf("$%d", len(args)))
			}
		}
		if c.Kind == "create" {
			if len(cols) == 0 {
				return []stmt{{sql: "insert into " + ident(t.Name) + " default values", table: t}}, nil
			}
			return []stmt{{sql: "insert into " + ident(t.Name) + " (" + strings.Join(cols, ", ") + ") values (" + strings.Join(vals, ", ") + ")", args: args, table: t}}, nil
		}
		if len(cols) == 0 {
			return nil, nil
		}
		set := make([]string, len(cols))
		for i := range cols {
			set[i] = cols[i] + " = " + vals[i]
		}
		args = append(args, ints(x.ids))
		return []stmt{{sql: fmt.Sprintf("update %s set %s where id = any($%d::bigint[])", ident(t.Name), strings.Join(set, ", "), len(args)), args: args, table: t}}, nil
	case "delete":
		var out []stmt
		for _, ch := range t.children {
			out = append(out, stmt{sql: fmt.Sprintf("delete from %s where %s = any($1::bigint[])", ident(ch.Name), ident(ch.ChildFK)), args: []any{ints(x.ids)}, table: ch})
		}
		return append(out, stmt{sql: "delete from " + ident(t.Name) + " where id = any($1::bigint[])", args: []any{ints(x.ids)}, table: t}), nil
	}
	types := map[string]string{}
	for _, in := range c.Input {
		types[in.Name] = in.Type
	}
	var out []stmt
	for _, e := range c.effects {
		s := stmt{sql: e.sql, table: e.table}
		for _, name := range e.params {
			switch name {
			case "reach":
				s.args = append(s.args, x.reach())
			case "self":
				s.args = append(s.args, strconv.FormatInt(x.p.Self, 10))
			case "ids":
				s.args = append(s.args, ints(x.ids))
			default:
				s.args = append(s.args, pgValue(x.in[name], types[name]))
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// startRun inserts the run row; its job is queued by BeforeCommit.
func (x *call) startRun(ctx context.Context) (*Result, error) {
	input, _ := json.Marshal(x.in)
	b := &pgx.Batch{}
	b.Queue("begin")
	b.Queue("insert into run (name, input, principal_id, for_id) values ($1, $2::jsonb, $3::bigint, $4::bigint) returning id",
		x.c.Name, string(input), strconv.FormatInt(x.p.ID, 10), optInt(x.p.forID()))
	br := x.conn.SendBatch(ctx, b)
	var id int64
	_, err := br.Exec()
	if err == nil {
		err = br.QueryRow().Scan(&id)
	}
	if cerr := br.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	res := &Result{Changed: []Changed{{Table: "run", IDs: []int64{id}}}, IDs: []int64{}, Run: &id}
	b = &pgx.Batch{}
	if err := x.beforeCommit(ctx, b, res, []Row{{"run", id, "run", id}}); err != nil {
		return nil, err
	}
	b.Queue("select pg_notify('sor_live', $1)", livePayload("run", []int64{id}, []int64{x.p.ID}, x.c.Name, x.id))
	b.Queue("commit")
	return res, x.conn.SendBatch(ctx, b).Close()
}

func projRows(br pgx.BatchResults) (map[int64]json.RawMessage, error) {
	rs, err := br.Query()
	if err != nil {
		return nil, err
	}
	m := map[int64]json.RawMessage{}
	for rs.Next() {
		var id int64
		var row []byte
		if err := rs.Scan(&id, &row); err != nil {
			return nil, err
		}
		m[id] = row
	}
	return m, rs.Err()
}

func livePayload(table string, ids, anchors []int64, command, call string) string {
	m := map[string]any{"t": table, "c": command, "e": call}
	if anchors != nil {
		m["a"] = anchors
	}
	if len(ids) <= 200 {
		m["ids"] = ids
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// pgProblem maps Postgres errors raised by a request to the kernel codes.
func pgProblem(err error) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	switch {
	case strings.HasPrefix(pe.Code, "23"):
		p := problem(409, "constraint", pe.Message)
		p.Constraint = pe.ConstraintName
		return p
	case strings.HasPrefix(pe.Code, "22"):
		return problem(422, "invalid", pe.Message)
	}
	return err
}

func asSlice(v any) []any { s, _ := v.([]any); return s }

func optInt(p *int64) any {
	if p == nil {
		return nil
	}
	return strconv.FormatInt(*p, 10)
}

func literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func uniq(xs []int64) []int64 {
	xs = slices.Clone(xs)
	slices.Sort(xs)
	return slices.Compact(xs)
}

func sortedKeys64[V any](m map[int64]V) []int64 {
	ks := make([]int64, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
