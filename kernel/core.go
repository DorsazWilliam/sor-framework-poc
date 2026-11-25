package kernel

import (
	"context"
	_ "embed"
	"time"
)

//go:embed schema.sql
var coreSchema string

// Core is the kernel's own module; it comes first. The rows of run and saved_view are their owner's; the
// capabilities on file, connection, delivery and delivery_result are set by the module that owns their policy,
// declaring the table again (options add up), as it adds options to a command by declaring it again.
var Core = Module{Name: "kernel", Schema: coreSchema, Register: func(r *Registry) {
	r.Table("file", NoCRUD(), Label("name"))
	r.Table("run", Anchor("owner_id", "self"), Read(Any), Write(Any), NoCRUD())
	r.Table("saved_view", Anchor("owner_id", "self"), Read(Any), Write(Any))
	r.Table("connection", NoCRUD())
	r.Table("delivery", AppendOnly(), NoCRUD(), Label("path"))
	r.Table("delivery_result", ChildOf("delivery", "delivery_id"), AppendOnly(), NoCRUD(), Label("status"))
	r.Command("run.cancel", On("run"), Effect("RunCancel"))
	r.Command("delivery.result", On("delivery"), Effect("DeliveryResult"))
	r.Command("delivery.retry", On("delivery"))
	r.Reaction("delivery.retry", "delivery.send")
	r.OnInsert("delivery", "delivery.send")
	r.Job("run.execute", runExecute)
	r.Job("delivery.send", deliverySend)
	r.Job("event.partition", func(ctx context.Context, k *Kernel, _ Job) error { return EnsurePartitions(ctx, k.Pool) },
		Every(30*24*time.Hour), Unique())
	r.Job("file.gc", fileGC, Every(24*time.Hour), Unique())
}}
