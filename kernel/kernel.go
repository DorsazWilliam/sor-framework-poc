package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kernel is the registry bound to a database.
type Kernel struct {
	*Static
	Pool *pgxpool.Pool
	Meta Meta

	// BeforeCommit, if set, runs inside a command's transaction after its checks; what it queues on b
	// is sent with the commit, in the same round trip.
	BeforeCommit func(ctx context.Context, b *pgx.Batch, c *Commit) error

	tables    map[string]*Table
	views     map[string]*View
	commands  map[string]*Command
	types     map[uint32]pgType
	refs      map[string]string
	principal *Query
	hub       hub
	verify    func(ctx context.Context, raw string) ([]string, error)
}

// Commit describes a command about to commit.
type Commit struct {
	Command   string
	Call      string
	Principal int64
	For       *int64
	Subjects  []string
	Roots     []int64 // roots of the command
	Rows      []Row   // rows changed
	Run       *int64
}

// Row is one changed row, as returned by an effect.
type Row struct {
	Table     string
	ID        int64
	RootTable string
	RootID    int64
}

var schemaRegistry = huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)

// NewPool opens a pool set up for the kernel: unnamed statements with text params (Postgres infers
// every type, plans are custom), no ping on acquire, round trips counted per request.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.ConnConfig.Tracer = tracer{}
	cfg.ShouldPing = func(context.Context, pgxpool.ShouldPingParams) bool { return false }
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Timing counts the database round trips and time of a request.
type Timing struct {
	RT atomic.Int64
	DB atomic.Int64 // nanoseconds
}

type timingKey struct{}
type startKey struct{}

// WithTiming returns a context whose database round trips are counted in the returned Timing.
func WithTiming(ctx context.Context) (context.Context, *Timing) {
	t := &Timing{}
	return context.WithValue(ctx, timingKey{}, t), t
}

func start(ctx context.Context) context.Context {
	t, _ := ctx.Value(timingKey{}).(*Timing)
	if t == nil {
		return ctx
	}
	t.RT.Add(1)
	return context.WithValue(ctx, startKey{}, time.Now())
}

func end(ctx context.Context) {
	t, _ := ctx.Value(timingKey{}).(*Timing)
	if s, ok := ctx.Value(startKey{}).(time.Time); ok && t != nil {
		t.DB.Add(int64(time.Since(s)))
	}
}

type tracer struct{}

func (tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return start(ctx)
}
func (tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) { end(ctx) }
func (tracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return start(ctx)
}
func (tracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData)     {}
func (tracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchEndData) { end(ctx) }

// Principal is the authenticated caller, merged with the human it acts for.
type Principal struct {
	ID        int64
	Kind      string
	Name      string
	Lang      string
	Functions []string
	Reach     map[string][]int64
	Self      int64
	For       *MeFor
	Subjects  []string
}

type principalRow struct {
	Subject   string             `json:"subject"`
	ID        int64              `json:"id"`
	Kind      string             `json:"kind"`
	Name      string             `json:"name"`
	Lang      string             `json:"lang"`
	Functions []string           `json:"functions"`
	Reach     map[string][]int64 `json:"reach"`
}

// Principal resolves subjects ([sub] or [sub, act_for]) in one round trip; an agent acting for a human
// gets the intersection of both functions and reaches, and @self is the human.
func (k *Kernel) Principal(ctx context.Context, subjects []string) (*Principal, error) {
	var raw []byte
	err := k.Pool.QueryRow(ctx, "select coalesce(json_agg(p), '[]') from ("+k.principal.Text+") p", pgArray(subjects)).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var rows []principalRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	find := func(s string) *principalRow {
		for i := range rows {
			if rows[i].Subject == s {
				return &rows[i]
			}
		}
		return nil
	}
	a := find(subjects[0])
	if a == nil {
		return nil, problem(403, "forbidden", "unknown subject "+subjects[0])
	}
	p := &Principal{ID: a.ID, Kind: a.Kind, Name: a.Name, Lang: a.Lang, Functions: a.Functions, Reach: a.Reach, Self: a.ID, Subjects: subjects}
	if len(subjects) > 1 {
		h := find(subjects[1])
		if h == nil {
			return nil, problem(403, "forbidden", "unknown subject "+subjects[1])
		}
		p.Self, p.Lang, p.For = h.ID, h.Lang, &MeFor{h.ID, h.Name}
		p.Functions = slices.DeleteFunc(slices.Clone(a.Functions), func(f string) bool { return !slices.Contains(h.Functions, f) })
		p.Reach = map[string][]int64{}
		for name, ids := range a.Reach {
			p.Reach[name] = slices.DeleteFunc(slices.Clone(ids), func(id int64) bool { return !slices.Contains(h.Reach[name], id) })
		}
	}
	if p.Functions == nil {
		p.Functions = []string{}
	}
	if p.Reach == nil {
		p.Reach = map[string][]int64{}
	}
	return p, nil
}

// Me is the principal as served by GET /api/me.
func (p *Principal) Me() Me {
	return Me{ID: p.ID, Name: p.Name, Kind: p.Kind, Lang: p.Lang, Functions: p.Functions, Reach: p.Reach, For: p.For}
}

func (p *Principal) can(fn string) *Problem {
	if fn == "" || slices.Contains(p.Functions, fn) {
		return nil
	}
	pr := problem(403, "forbidden", "needs "+fn)
	pr.Needs = fn
	return pr
}

func (p *Principal) forID() *int64 {
	if p.For == nil {
		return nil
	}
	return &p.For.ID
}

// pgArray renders a Postgres array literal of text elements.
func pgArray[T any](xs []T) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		s := fmt.Sprint(x)
		parts[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func ints(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// View returns the registry view name, or nil.
func (k *Kernel) ViewByName(name string) *View { return k.views[name] }

// CommandByName returns the registry command name, or nil.
func (k *Kernel) CommandByName(name string) *Command { return k.commands[name] }
