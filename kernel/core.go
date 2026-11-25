package kernel

import _ "embed"

//go:embed schema.sql
var coreSchema string

// Core is the kernel's own module; it comes first.
var Core = Module{Name: "kernel", Schema: coreSchema, Register: func(r *Registry) {
	r.Table("file", NoCRUD(), Label("name"))
	r.Table("run", Anchor("principal_id", "self"), NoCRUD())
	r.Table("saved_view", Anchor("owner_id", "self"))
	r.Table("connection", NoCRUD())
	r.Table("delivery", AppendOnly(), NoCRUD(), Label("kind"))
	r.Table("delivery_result", ChildOf("delivery", "delivery_id"), AppendOnly(), NoCRUD(), Label("status"))
	r.Command("run.cancel", On("run"), Effect("RunCancel"))
}}
