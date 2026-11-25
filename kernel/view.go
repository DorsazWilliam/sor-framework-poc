package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const defaultLimit, maxLimit = 200, 5000

// ViewRequest is the body of POST /api/v/{view}.
type ViewRequest struct {
	Params map[string]any `json:"params,omitempty"`
	Wrap
}

// decode validates body against s and decodes it into dst.
func decode(s interface{ validate(any) *Problem }, body []byte, dst any) *Problem {
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return problem(422, "invalid", err.Error())
	}
	if p := s.validate(v); p != nil {
		return p
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return problem(422, "invalid", err.Error())
	}
	return nil
}

func (v *View) validate(x any) *Problem    { return validate(v.in, x) }
func (c *Command) validate(x any) *Problem { return validate(c.in, x) }

// View runs a wrapped view and returns the JSON array produced by Postgres (one round trip).
func (k *Kernel) View(ctx context.Context, p *Principal, name string, body []byte) ([]byte, error) {
	v := k.views[name]
	if v == nil {
		return nil, problem(404, "not_found", "no view "+name)
	}
	if pr := p.can(v.Needs); pr != nil {
		return nil, pr
	}
	var req ViewRequest
	if pr := decode(v, body, &req); pr != nil {
		return nil, pr
	}
	params := map[string]Col{}
	for _, c := range v.Params {
		params[c.Name] = c
	}
	var args []any
	for _, name := range v.params {
		switch name {
		case "reach":
			args = append(args, ints(p.Reach[reachName(v.Anchor)]))
		case "self":
			args = append(args, strconv.FormatInt(p.Self, 10))
		case "ids":
			args = append(args, nil)
		default:
			args = append(args, pgValue(req.Params[name], params[name].Type))
		}
	}
	sql, args, pr := v.wrap(req.Wrap, args)
	if pr != nil {
		return nil, pr
	}
	sql = "select coalesce(json_agg(t), '[]') from (" + sql + ") t"
	if !v.history {
		var out []byte
		return out, k.Pool.QueryRow(ctx, sql, args...).Scan(&out)
	}
	t := k.tables[fmt.Sprint(req.Params["root_table"])]
	if t == nil || t.root != t {
		return nil, problem(404, "not_found", "no root table")
	}
	if pr := p.can(t.Read); pr != nil {
		return nil, pr
	}
	args = append(args, pgValue(req.Params["root_id"], "int8"))
	guard := fmt.Sprintf("select exists(select 1 from %s r where r.id = $%d::bigint and %s)", ident(t.Name), len(args), t.reachFilter(len(args)+1))
	if t.Anchor != nil {
		args = append(args, ints(p.Reach[t.Anchor.Reach]))
	}
	var ok bool
	var out []byte
	if err := k.Pool.QueryRow(ctx, "select ("+guard+"), ("+sql+")", args...).Scan(&ok, &out); err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "not_found", "")
	}
	return out, nil
}

func reachName(a *AnchorDecl) string {
	if a == nil {
		return ""
	}
	return a.Reach
}

// wrap builds filter, sort, group, aggregate and page around the view query; values become params.
func (v *View) wrap(w Wrap, args []any) (string, []any, *Problem) {
	cols := map[string]Col{}
	for _, c := range v.Columns {
		cols[c.Name] = c
	}
	arg := func(x any) string {
		args = append(args, x)
		return fmt.Sprintf("$%d", len(args))
	}
	var where []string
	ops := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "le": "<=", "gt": ">", "ge": ">="}
	for _, f := range w.Filter {
		c, ok := cols[f.Col]
		if !ok {
			return "", nil, problem(422, "invalid", "unknown column "+f.Col)
		}
		col := "v." + ident(f.Col)
		switch f.Op {
		case "null":
			where = append(where, col+" is null")
		case "notnull":
			where = append(where, col+" is not null")
		case "in":
			if _, ok := f.Value.([]any); !ok {
				return "", nil, problem(422, "invalid", "in needs an array value")
			}
			where = append(where, col+" = any("+arg(pgValue(f.Value, "_"+c.Type))+")")
		case "contains":
			s := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(fmt.Sprint(f.Value))
			where = append(where, col+"::text ilike '%' || "+arg(s)+" || '%'")
		default:
			if f.Value == nil {
				return "", nil, problem(422, "invalid", f.Op+" needs a value")
			}
			where = append(where, col+" "+ops[f.Op]+" "+arg(pgValue(f.Value, c.Type)))
		}
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " where " + strings.Join(where, " and ")
	}
	grouped := len(w.Group)+len(w.Agg) > 0
	out := slices.Collect(func(yield func(string) bool) {
		for _, c := range v.Columns {
			yield(c.Name)
		}
	})
	sel := "v.*"
	if grouped {
		var list []string
		out = nil
		for _, g := range w.Group {
			list, out = append(list, "v."+ident(g)), append(out, g)
		}
		for _, a := range w.Agg {
			if a.Fn == "count" {
				list, out = append(list, `count(*) as "count"`), append(out, "count")
				continue
			}
			if _, ok := cols[a.Col]; !ok {
				return "", nil, problem(422, "invalid", "unknown column "+a.Col)
			}
			n := a.Fn + "_" + a.Col
			list, out = append(list, fmt.Sprintf("%s(v.%s) as %s", a.Fn, ident(a.Col), ident(n))), append(out, n)
		}
		sel = strings.Join(list, ", ")
	}
	var order []string
	for _, s := range w.Sort {
		if !slices.Contains(out, s.Col) {
			return "", nil, problem(422, "invalid", "cannot sort on "+s.Col)
		}
		dir := "asc"
		if s.Dir == "desc" {
			dir = "desc"
		}
		order = append(order, ident(s.Col)+" "+dir)
	}
	_, hasID := cols["id"]
	byID := !grouped && hasID
	if byID && !slices.ContainsFunc(w.Sort, func(s Sort) bool { return s.Col == "id" }) {
		order = append(order, `"id" asc`)
	}
	orderSQL := ""
	if len(order) > 0 {
		orderSQL = " order by v." + strings.Join(order, ", v.")
		if grouped {
			orderSQL = " order by " + strings.Join(order, ", ")
		}
	}
	limit := w.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	page := " limit " + arg(strconv.Itoa(min(limit, maxLimit))) + " offset " + arg(strconv.Itoa(max(w.Offset, 0)))
	q := "(" + v.sql + ") v"
	switch {
	case grouped:
		groupSQL := ""
		if len(w.Group) > 0 {
			groupSQL = " group by " + strings.Join(slices.Collect(func(yield func(string) bool) {
				for _, g := range w.Group {
					yield("v." + ident(g))
				}
			}), ", ")
		}
		return "select " + sel + " from " + q + whereSQL + groupSQL + orderSQL + page, args, nil
	case byID:
		return "with k as materialized (select v.id from " + q + whereSQL + orderSQL + page + ") select v.* from k join " + q +
			" using (id)" + orderSQL, args, nil
	default:
		return "select v.* from " + q + whereSQL + orderSQL + page, args, nil
	}
}

// pgValue renders a JSON value as the text form Postgres parses for type typ; nil stays NULL.
func pgValue(v any, typ string) any {
	if v == nil {
		return nil
	}
	if typ == "json" || typ == "jsonb" {
		b, _ := json.Marshal(v)
		return string(b)
	}
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		return arrayLiteral(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func arrayLiteral(xs []any) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		switch x := x.(type) {
		case nil:
			parts[i] = "NULL"
		case []any:
			parts[i] = arrayLiteral(x)
		default:
			parts[i] = pgArray([]any{pgValue(x, "")})[1:]
			parts[i] = parts[i][:len(parts[i])-1]
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}
