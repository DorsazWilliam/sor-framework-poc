// Package testmod is the fixture module of the kernel tests.
package testmod

import (
	_ "embed"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:generate docker run --rm -v $PWD/..:/src -w /src/testmod sqlc/sqlc:1.31.1 generate

//go:embed schema.sql
var schema string

// Manifest is the sqlc manifest of the kernel and this module.
//
//go:embed sqlc.json
var Manifest []byte

var Module = kernel.Module{Name: "testmod", Schema: schema, Register: func(r *kernel.Registry) {
	r.Table("org", kernel.Write("admin"))
	r.Table("person", kernel.Write("admin"))
	r.Table("org_lock", kernel.ChildOf("org", "org_id"), kernel.Write("admin"))
	r.Table("entry", kernel.Under("org"), kernel.Anchor("person_id", "user"), kernel.Write("entry"), kernel.Guarded("validated"), kernel.Label("note"))
	r.Table("entry_line", kernel.ChildOf("entry", "entry_id"), kernel.Write("entry"), kernel.Label("label"))
	r.Action("entry", "editable", "not_locked", "not_validated", "short")
	r.Action("entry", "deletable", "not_locked", "not_validated")
	r.Action("entry", "validatable", "not_validated")
	r.Command("entry.validate", kernel.On("entry"), kernel.When("validatable"), kernel.Effect("EntryLockRows", "EntryValidate"),
		kernel.Needs("validate"), kernel.Reach("team"))
	r.Screen(kernel.Screen{ID: "entries", Group: "time", Views: []string{"entry_grid"}, Table: "entry", Commands: []string{"entry.validate"}})
}}
