package kernel

import (
	"context"
	_ "embed"
	"time"
)

//go:embed schema.sql
var coreSchema string

// Core is the kernel's own module; it comes first. Its commands on delivery need the Write capability
// of delivery, which a domain module sets by declaring the table again; it adds options to a command by
// declaring it again.
var Core = Module{Name: "kernel", Schema: coreSchema, Register: func(r *Registry) {
	r.Table("file", NoCRUD(), Label("name"))
	r.Table("run", Anchor("principal_id", "self"), NoCRUD())
	r.Table("saved_view", Anchor("owner_id", "self"))
	r.Table("connection", NoCRUD())
	r.Table("delivery", AppendOnly(), NoCRUD(), Label("kind"))
	r.Table("delivery_result", ChildOf("delivery", "delivery_id"), AppendOnly(), NoCRUD(), Label("status"))
	r.Command("run.cancel", On("run"), Effect("RunCancel"))
	r.Command("delivery.result", On("delivery"), Effect("DeliveryResult"))
	r.Command("delivery.retry", On("delivery"))
	r.Reaction("delivery.retry", "delivery.send")
	r.Job("run.execute", runExecute)
	r.Job("delivery.send", deliverySend)
	r.Job("event.partition", func(ctx context.Context, k *Kernel, _ Job) error { return EnsurePartitions(ctx, k.Pool) },
		Every(30*24*time.Hour), Unique())
	r.Job("file.gc", fileGC, Every(24*time.Hour), Unique())
}}
