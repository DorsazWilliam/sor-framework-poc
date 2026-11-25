package kernel

import (
	"encoding/json"
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// The JSON contract of GET /api/meta (SPEC §13).

type Col struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Format      string `json:"format"`
	Nullable    bool   `json:"nullable"`
	Ref         string `json:"ref,omitempty"`
	Enum        string `json:"enum,omitempty"`
	Description string `json:"description,omitempty"`
	required    bool
}

type TableCol struct {
	Col
	Guarded    bool `json:"guarded"`
	HasDefault bool `json:"has_default"`
	generated  bool
	def        string // default expression
}

type Table struct {
	Name        string              `json:"name"`
	Root        string              `json:"root"`
	ChildFK     string              `json:"child_fk,omitempty"`
	Under       string              `json:"under,omitempty"`
	Anchor      *AnchorDecl         `json:"anchor,omitempty"`
	Read        string              `json:"read"`
	Write       string              `json:"write"`
	Label       string              `json:"label"`
	CRUD        bool                `json:"crud"`
	Columns     []TableCol          `json:"columns"`
	Conditions  []string            `json:"conditions"`
	Actions     map[string][]string `json:"actions"`
	Description string              `json:"description,omitempty"`

	decl     *TableDecl
	root     *Table
	source   string // projection view, or the table itself
	children []*Table
}

type View struct {
	Name        string      `json:"name"`
	Params      []Col       `json:"params"`
	Columns     []Col       `json:"columns"`
	Roots       []string    `json:"roots"`
	Needs       string      `json:"needs"`
	Anchor      *AnchorDecl `json:"anchor,omitempty"`
	Description string      `json:"description,omitempty"`

	sql     string
	params  []string            // by $n
	actions map[string][]string // actions of its roots whose conditions are columns of the view
	history bool
	in, out *huma.Schema
}

type Command struct {
	Name        string `json:"name"`
	Table       string `json:"table"`
	Kind        string `json:"kind"`
	Rootless    bool   `json:"rootless"`
	Input       []Col  `json:"input"`
	When        string `json:"when,omitempty"`
	Ensure      string `json:"ensure,omitempty"`
	Reach       string `json:"reach,omitempty"`
	Needs       string `json:"needs"`
	Description string `json:"description,omitempty"`

	table   *Table
	ensureT *Table // table whose touched roots Ensure reads
	ensureA string
	effects []*effect
	run     *RunDecl
	in      *huma.Schema
}

type Meta struct {
	Tables    []*Table            `json:"tables"`
	Views     []*View             `json:"views"`
	Commands  []*Command          `json:"commands"`
	Screens   []Screen            `json:"screens"`
	Enums     map[string][]string `json:"enums"`
	Functions []string            `json:"functions"`
	Groups    []string            `json:"groups"`
}

// Permitted is the registry served to p (GET /api/meta, MCP): the views and commands its capability permits,
// the tables it reads or those commands act on (with their roots), the screens left with a view.
func (k *Kernel) Permitted(p *Principal) Meta {
	m := k.Meta
	m.Views = slices.DeleteFunc(slices.Clone(m.Views), func(v *View) bool { return p.can(v.Needs) != nil })
	m.Commands = slices.DeleteFunc(slices.Clone(m.Commands), func(c *Command) bool { return p.can(c.Needs) != nil })
	used := map[*Table]bool{}
	for _, c := range m.Commands {
		if c.table != nil {
			used[c.table], used[c.table.root] = true, true
		}
	}
	m.Tables = slices.DeleteFunc(slices.Clone(m.Tables), func(t *Table) bool { return !used[t] && p.can(t.Read) != nil })
	m.Screens = permittedScreens(m.Screens, m.Views, m.Commands)
	m.Groups = slices.DeleteFunc(slices.Clone(m.Groups), func(g string) bool {
		return !slices.ContainsFunc(m.Screens, func(s Screen) bool { return s.Group == g })
	})
	return m
}

// permittedScreens keeps the screens with a permitted view, their permitted views and commands.
func permittedScreens(ss []Screen, views []*View, commands []*Command) []Screen {
	out := []Screen{}
	for _, s := range ss {
		s.Views = slices.DeleteFunc(slices.Clone(s.Views), func(n string) bool {
			return !slices.ContainsFunc(views, func(v *View) bool { return v.Name == n })
		})
		s.Commands = slices.DeleteFunc(slices.Clone(s.Commands), func(n string) bool {
			return !slices.ContainsFunc(commands, func(c *Command) bool { return c.Name == n })
		})
		if len(s.Views) > 0 {
			s.Children = permittedScreens(s.Children, views, commands)
			out = append(out, s)
		}
	}
	return out
}

// Me is the JSON of GET /api/me.
type Me struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Kind      string             `json:"kind"`
	Lang      string             `json:"lang"`
	Functions []string           `json:"functions"`
	Reach     map[string][]int64 `json:"reach"`
	For       *MeFor             `json:"for,omitempty"`
}

type MeFor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Problem is an RFC 9457 error body extended with the kernel codes.
type Problem struct {
	Status     int            `json:"status"`
	Title      string         `json:"title"`
	Code       string         `json:"code"`
	Detail     string         `json:"detail,omitempty"`
	Needs      string         `json:"needs,omitempty"`
	Action     string         `json:"action,omitempty"`
	Rejected   []Rejected     `json:"rejected,omitempty"`
	Constraint string         `json:"constraint,omitempty"`
	Errors     []ProblemError `json:"errors,omitempty"`
}

type Rejected struct {
	ID         int64           `json:"id"`
	Conditions []string        `json:"conditions"`
	Row        json.RawMessage `json:"row"`
}

type ProblemError struct {
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
	Value    any    `json:"value,omitempty"`
}

func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Code + ": " + p.Detail
	}
	return p.Code
}

func problem(status int, code, detail string) *Problem {
	titles := map[int]string{401: "Unauthorized", 403: "Forbidden", 404: "Not Found", 409: "Conflict", 422: "Unprocessable Entity"}
	return &Problem{Status: status, Title: titles[status], Code: code, Detail: detail}
}

// Result is the body of a successful command.
type Result struct {
	Call    string    `json:"call"` // the e of the live keys of the command
	Changed []Changed `json:"changed"`
	IDs     []int64   `json:"ids"`
	Run     *int64    `json:"run,omitempty"`
}

type Changed struct {
	Table string  `json:"table"`
	IDs   []int64 `json:"ids"`
}
