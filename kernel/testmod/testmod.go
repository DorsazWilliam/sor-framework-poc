// Package testmod is the fixture module of the kernel tests.
package testmod

import (
	"context"
	_ "embed"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:generate docker run --rm -v $PWD/..:/src -w /src/testmod sqlc/sqlc:1.31.1 generate

//go:embed schema.sql
var schema string

// Manifest is the sqlc manifest of the kernel and this module.
//
//go:embed sqlc.json
var Manifest []byte

// Ticks counts the runs of the periodic job tick; TickBy is the principal it ran as.
var Ticks, TickBy atomic.Int64

var Module = kernel.Module{Name: "testmod", Schema: schema, Register: func(r *kernel.Registry) {
	r.Table("org", kernel.Write("admin"))
	r.Table("person", kernel.Write("admin"))
	r.Table("org_lock", kernel.ChildOf("org", "org_id"), kernel.Write("admin"))
	r.Table("entry", kernel.Under("org"), kernel.Anchor("person_id", "user"), kernel.Write("entry"), kernel.Guarded("validated"), kernel.Label("note"))
	r.Table("entry_line", kernel.ChildOf("entry", "entry_id"), kernel.Write("entry"), kernel.Label("label"))
	r.Table("delivery", kernel.Write("validate"))
	for _, c := range []string{"principal_id", "for_id", "owner_id"} {
		r.Ref(c, "person")
	}
	r.Action("entry", "editable", "not_locked", "not_validated", "short")
	r.Action("entry", "deletable", "not_locked", "not_validated")
	r.Action("entry", "validatable", "not_validated")
	r.Command("entry.validate", kernel.On("entry"), kernel.When("validatable"), kernel.Effect("EntryLockRows", "EntryValidate"),
		kernel.Needs("validate"), kernel.Reach("team"))
	r.Command("entry.ping", kernel.On("entry"), kernel.When("editable"))
	r.Reaction("entry.ping", "entry.mark")
	r.Command("entry.export", kernel.On("entry"), kernel.Effect("EntryExport"))
	r.Run("entry.run", "entry_run_candidates", "entry.create", kernel.Needs("admin"))
	r.Job("entry.mark", mark)
	r.Job("tick", tick, kernel.Every(250*time.Millisecond), kernel.As("bot"))
	r.Screen(kernel.Screen{ID: "entries", Group: "time", Views: []string{"entry_grid"}, Table: "entry", Commands: []string{"entry.validate"},
		Children: []kernel.Screen{{ID: "entry_lines", Views: []string{"entry_line"}, Table: "entry_line", Link: map[string]string{"entry_id": "id"}}}})
}}

// mark notes on the entries of an entry.ping, as the principal who pinged.
func mark(ctx context.Context, k *kernel.Kernel, job kernel.Job) error {
	var a kernel.ReactionArgs
	if err := json.Unmarshal(job.Args, &a); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"ids": a.IDs, "note": "marked " + a.Call})
	_, err := k.Call(ctx, job.Principal, "entry.update", body)
	return err
}

func tick(_ context.Context, _ *kernel.Kernel, job kernel.Job) error {
	Ticks.Add(1)
	TickBy.Store(job.Principal.ID)
	return nil
}
