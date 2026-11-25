package kernel

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// colSchema is the JSON schema of a Postgres-typed value.
func (k *Kernel) colSchema(c Col) *huma.Schema {
	s := k.typeSchema(c.Type)
	s.Nullable = c.Nullable
	s.Description = c.Description
	return s
}

func (k *Kernel) typeSchema(typ string) *huma.Schema {
	switch typ {
	case "int2", "int4", "int8":
		return &huma.Schema{Type: huma.TypeInteger, Format: "int64"}
	case "numeric", "float4", "float8":
		return &huma.Schema{Type: huma.TypeNumber}
	case "bool":
		return &huma.Schema{Type: huma.TypeBoolean}
	case "date":
		return &huma.Schema{Type: huma.TypeString, Format: "date"}
	case "timestamptz", "timestamp":
		return &huma.Schema{Type: huma.TypeString, Format: "date-time"}
	case "uuid":
		return &huma.Schema{Type: huma.TypeString, Format: "uuid"}
	case "json", "jsonb":
		return &huma.Schema{}
	}
	if labels, ok := k.Meta.Enums[typ]; ok {
		s := &huma.Schema{Type: huma.TypeString}
		for _, l := range labels {
			s.Enum = append(s.Enum, l)
		}
		return s
	}
	if elem, ok := strings.CutPrefix(typ, "_"); ok {
		return &huma.Schema{Type: huma.TypeArray, Items: k.typeSchema(elem)}
	}
	return &huma.Schema{Type: huma.TypeString}
}

func object(props map[string]*huma.Schema, required []string) *huma.Schema {
	return &huma.Schema{Type: huma.TypeObject, Properties: props, Required: required, AdditionalProperties: false}
}

func enumOf(values ...string) *huma.Schema {
	s := &huma.Schema{Type: huma.TypeString}
	for _, v := range values {
		s.Enum = append(s.Enum, v)
	}
	return s
}

func (k *Kernel) rowSchema(cols []Col) *huma.Schema {
	props := map[string]*huma.Schema{}
	var req []string
	for _, c := range cols {
		props[c.Name] = k.colSchema(c)
		req = append(req, c.Name)
	}
	return object(props, req)
}

func (b *booter) viewSchemas(v *View) {
	params := map[string]*huma.Schema{}
	var names []string
	for _, p := range v.Params {
		params[p.Name] = b.colSchema(p)
	}
	for _, c := range v.Columns {
		names = append(names, c.Name)
	}
	col := enumOf(names...)
	one, zero, max := 1.0, 0.0, float64(maxLimit)
	v.in = object(map[string]*huma.Schema{
		"params": object(params, nil),
		"filter": {Type: huma.TypeArray, Items: object(map[string]*huma.Schema{
			"col": col, "op": enumOf("eq", "ne", "lt", "le", "gt", "ge", "in", "contains", "null", "notnull"), "value": {}}, []string{"col", "op"})},
		"sort":   {Type: huma.TypeArray, Items: object(map[string]*huma.Schema{"col": {Type: huma.TypeString}, "dir": enumOf("asc", "desc")}, []string{"col"})},
		"group":  {Type: huma.TypeArray, Items: col},
		"agg":    {Type: huma.TypeArray, Items: object(map[string]*huma.Schema{"col": col, "fn": enumOf("sum", "count", "min", "max", "avg")}, []string{"fn"})},
		"limit":  {Type: huma.TypeInteger, Minimum: &one, Maximum: &max},
		"offset": {Type: huma.TypeInteger, Minimum: &zero},
	}, nil)
	v.out = &huma.Schema{Type: huma.TypeArray, Items: b.rowSchema(v.Columns)}
	precompute(v.in)
}

func (b *booter) commandSchema(c *Command) {
	props := map[string]*huma.Schema{}
	var req []string
	if !c.Rootless && c.Kind != "create" && c.Kind != "run" {
		one := 1
		props["ids"] = &huma.Schema{Type: huma.TypeArray, MinItems: &one, Items: &huma.Schema{Type: huma.TypeInteger, Format: "int64"}}
		req = append(req, "ids")
	}
	for _, in := range c.Input {
		props[in.Name] = b.colSchema(in)
		if in.required {
			req = append(req, in.Name)
		}
	}
	c.in = object(props, req)
	precompute(c.in)
}

var resultSchema = object(map[string]*huma.Schema{
	"call": {Type: huma.TypeString, Format: "uuid"},
	"changed": {Type: huma.TypeArray, Items: object(map[string]*huma.Schema{
		"table": {Type: huma.TypeString},
		"ids":   {Type: huma.TypeArray, Items: &huma.Schema{Type: huma.TypeInteger, Format: "int64"}},
	}, []string{"table", "ids"})},
	"ids": {Type: huma.TypeArray, Items: &huma.Schema{Type: huma.TypeInteger, Format: "int64"}},
	"run": {Type: huma.TypeInteger, Format: "int64"},
}, []string{"call", "changed", "ids"})

func precompute(s *huma.Schema) {
	if s == nil {
		return
	}
	s.PrecomputeMessages()
	for _, p := range s.Properties {
		precompute(p)
	}
	precompute(s.Items)
}

// validate checks v against s and returns an `invalid` problem.
func validate(s *huma.Schema, v any) *Problem {
	res := &huma.ValidateResult{}
	huma.Validate(schemaRegistry, s, huma.NewPathBuffer([]byte(""), 0), huma.ModeWriteToServer, v, res)
	if len(res.Errors) == 0 {
		return nil
	}
	p := problem(422, "invalid", "validation failed")
	for _, e := range res.Errors {
		if d, ok := e.(*huma.ErrorDetail); ok {
			p.Errors = append(p.Errors, ProblemError{Message: d.Message, Location: d.Location, Value: d.Value})
		} else {
			p.Errors = append(p.Errors, ProblemError{Message: e.Error()})
		}
	}
	return p
}
