package kernel

import (
	"encoding/json"

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
	params  []string // by $n
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
