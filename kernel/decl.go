package kernel

import (
	"context"
	"encoding/json"
	"slices"
	"time"
)

// Module is a unit of SQL plus declarations.
type Module struct {
	Name     string
	Schema   string // the module's schema.sql
	Register func(r *Registry)
}

// TableDecl is what a module says about a table beyond the SQL.
type TableDecl struct {
	Name       string
	ChildOf    string // root table; "" = the table is a root
	ChildFK    string
	Under      string
	Anchor     *AnchorDecl
	Read       string
	Write      string
	Guarded    []string
	Label      string
	AppendOnly bool
	NoCRUD     bool
	Group      string // nav group of its generic screen; "" = none
}

// AnchorDecl ties rows to a named reach of the principal.
type AnchorDecl struct {
	Column string `json:"column"`
	Reach  string `json:"reach"`
}

// CommandDecl declares a command.
type CommandDecl struct {
	Name     string
	On       string
	Rootless bool
	When     string
	Ensure   string
	Effects  []string
	Needs    *string // nil = the Write capability of its table
	Reach    string
	Run      *RunDecl
}

// RunDecl makes a command a run: perRoot executed for every row of candidates.
type RunDecl struct {
	Candidates string
	PerRoot    string
}

// JobDecl declares a job kind.
type JobDecl struct {
	Kind   string
	Fn     JobFunc
	As     string
	Every  time.Duration
	Unique bool
}

// Job is one execution of a job kind.
type Job struct {
	Kind     string          `json:"kind"`
	Args     json.RawMessage `json:"args"`     // ReactionArgs for a reaction; null for a periodic job
	Subjects []string        `json:"subjects"` // As(subject), or the subjects of the command that inserted it
	// Principal is resolved from Subjects before the job runs; nil without subjects.
	Principal *Principal `json:"-"`
}

// JobFunc runs a job; it calls views and commands through k as job.Principal. An error retries the job.
type JobFunc func(ctx context.Context, k *Kernel, job Job) error

// Screen is a node of the UI tree (SPEC §19).
type Screen struct {
	ID        string            `json:"id"`
	Group     string            `json:"group"`
	Views     []string          `json:"views"`
	Defaults  map[string]string `json:"defaults"`
	Table     string            `json:"table,omitempty"`
	Commands  []string          `json:"commands"`
	Wrap      *Wrap             `json:"wrap,omitempty"`
	Children  []Screen          `json:"children"`
	Link      map[string]string `json:"link,omitempty"`
	Component string            `json:"component,omitempty"`
}

// Wrap is the filter, sort, group, aggregate and page applied around a view.
type Wrap struct {
	Filter []Filter `json:"filter,omitempty"`
	Sort   []Sort   `json:"sort,omitempty"`
	Group  []string `json:"group,omitempty"`
	Agg    []Agg    `json:"agg,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Offset int      `json:"offset,omitempty"`
}

// Filter is a condition on a column, or an action: the rows where every condition of the action is true.
type Filter struct {
	Col    string `json:"col,omitempty"`
	Op     string `json:"op,omitempty"`
	Value  any    `json:"value,omitempty"`
	Action string `json:"action,omitempty"`
}

type Sort struct {
	Col string `json:"col"`
	Dir string `json:"dir"`
}

type Agg struct {
	Col string `json:"col"`
	Fn  string `json:"fn"`
}

// Sum is the sum of each column, the totals of a screen's wrap.
func Sum(cols ...string) []Agg {
	aggs := make([]Agg, len(cols))
	for i, c := range cols {
		aggs[i] = Agg{Col: c, Fn: "sum"}
	}
	return aggs
}

// Registry collects the declarations of all modules.
type Registry struct {
	Tables    map[string]*TableDecl
	tableSeq  []string
	Actions   map[string]map[string][]string // table → action → conditions
	Commands  []*CommandDecl
	Jobs      []*JobDecl
	Reactions map[string][]string // command → job kinds
	OnInserts map[string][]string // table → job kinds, a job per inserted row
	Screens   []Screen
	Refs      map[string]string // column name → referenced table, for columns that are not foreign keys
}

func newRegistry() *Registry {
	return &Registry{Tables: map[string]*TableDecl{}, Actions: map[string]map[string][]string{}, Reactions: map[string][]string{},
		OnInserts: map[string][]string{}, Refs: map[string]string{}}
}

// Ref gives every column named column a reference to table, like a foreign key column (stamps such as
// principal_id that carry no FK).
func (r *Registry) Ref(column, table string) { r.Refs[column] = table }

type TableOpt func(*TableDecl)

// Table declares a table; declaring it again adds options.
func (r *Registry) Table(name string, opts ...TableOpt) {
	t := r.Tables[name]
	if t == nil {
		t = &TableDecl{Name: name}
		r.Tables[name] = t
		r.tableSeq = append(r.tableSeq, name)
	}
	for _, o := range opts {
		o(t)
	}
}

func Root() TableOpt                   { return func(t *TableDecl) { t.ChildOf, t.ChildFK = "", "" } }
func ChildOf(root, fk string) TableOpt { return func(t *TableDecl) { t.ChildOf, t.ChildFK = root, fk } }
func Under(root string) TableOpt       { return func(t *TableDecl) { t.Under = root } }
func Anchor(column, reach string) TableOpt {
	return func(t *TableDecl) { t.Anchor = &AnchorDecl{column, reach} }
}
// Any is the capability of every authenticated principal, said explicitly: nothing is open by default.
const Any = "any"

func Read(fn string) TableOpt  { return func(t *TableDecl) { t.Read = fn } }
func Write(fn string) TableOpt { return func(t *TableDecl) { t.Write = fn } }
func Guarded(columns ...string) TableOpt {
	return func(t *TableDecl) { t.Guarded = append(t.Guarded, columns...) }
}
func Label(column string) TableOpt { return func(t *TableDecl) { t.Label = column } }
func AppendOnly() TableOpt         { return func(t *TableDecl) { t.AppendOnly = true } }
func NoCRUD() TableOpt             { return func(t *TableDecl) { t.NoCRUD = true } }

// Group gives the table a generic screen in a nav group (SPEC §6 `Screen(group)`; renamed because Screen is the type of §19).
func Group(group string) TableOpt { return func(t *TableDecl) { t.Group = group } }

// Action declares an action of a table as the AND of conditions of its projection.
func (r *Registry) Action(table, name string, conditions ...string) {
	if r.Actions[table] == nil {
		r.Actions[table] = map[string][]string{}
	}
	r.Actions[table][name] = conditions
}

type CommandOpt func(*CommandDecl)

// Command declares a command; declaring it again adds options.
func (r *Registry) Command(name string, opts ...CommandOpt) {
	i := slices.IndexFunc(r.Commands, func(c *CommandDecl) bool { return c.Name == name })
	if i < 0 {
		r.Commands, i = append(r.Commands, &CommandDecl{Name: name}), len(r.Commands)
	}
	for _, o := range opts {
		o(r.Commands[i])
	}
}

func On(table string) CommandOpt      { return func(c *CommandDecl) { c.On = table } }
func Rootless() CommandOpt            { return func(c *CommandDecl) { c.Rootless = true } }
func When(action string) CommandOpt   { return func(c *CommandDecl) { c.When = action } }
func Ensure(action string) CommandOpt { return func(c *CommandDecl) { c.Ensure = action } }
func Effect(queries ...string) CommandOpt {
	return func(c *CommandDecl) { c.Effects = append(c.Effects, queries...) }
}
func Needs(fn string) CommandOpt   { return func(c *CommandDecl) { c.Needs = &fn } }
func Reach(name string) CommandOpt { return func(c *CommandDecl) { c.Reach = name } }

// Run declares a run: perRootCommand executed for each row of candidatesView.
func (r *Registry) Run(name, candidatesView, perRootCommand string, opts ...CommandOpt) {
	r.Command(name, append([]CommandOpt{func(c *CommandDecl) { c.Run = &RunDecl{candidatesView, perRootCommand} }}, opts...)...)
}

type JobOpt func(*JobDecl)

// Job declares a job kind.
func (r *Registry) Job(kind string, fn JobFunc, opts ...JobOpt) {
	j := &JobDecl{Kind: kind, Fn: fn}
	for _, o := range opts {
		o(j)
	}
	r.Jobs = append(r.Jobs, j)
}

func As(subject string) JobOpt     { return func(j *JobDecl) { j.As = subject } }
func Every(d time.Duration) JobOpt { return func(j *JobDecl) { j.Every = d } }
func Unique() JobOpt               { return func(j *JobDecl) { j.Unique = true } }

// Reaction inserts a job of jobKind when command commits.
func (r *Registry) Reaction(command, jobKind string) {
	r.Reactions[command] = append(r.Reactions[command], jobKind)
}

// OnInsert inserts a job of jobKind for every row a command inserts in table, at its commit (as Reaction).
func (r *Registry) OnInsert(table, jobKind string) {
	r.OnInserts[table] = append(r.OnInserts[table], jobKind)
}

// Screen declares a screen.
func (r *Registry) Screen(s Screen) { r.Screens = append(r.Screens, s) }
