// Package accounting declares the exchange with the accounting system: pull of clients and projects, push of invoices.
package accounting

import (
	_ "embed"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:embed schema.sql
var schema string

// Module is the accounting module.
var Module = kernel.Module{Name: "accounting", Schema: schema, Register: func(r *kernel.Registry) {
	sync := []kernel.TableOpt{kernel.Read("accounting_sync"), kernel.Write("accounting_sync")}
	r.Table("connection", kernel.Read("accounting_sync"), kernel.Write("master_data"))
	r.Table("delivery", sync...)
	r.Table("delivery_result", sync...)

	r.Command("invoice.export", kernel.On("invoice"), kernel.When("exportable"), kernel.Effect("InvoiceExport"), kernel.Needs("invoice"))
	r.Command("connection.pull", kernel.On("connection"), kernel.Needs("master_data"))
	r.Job("accounting.pull", accountingPull, kernel.As("svc-accounting"), kernel.Every(5*time.Minute), kernel.Unique())
	r.Reaction("connection.pull", "accounting.pull")
	r.Command("connection.advance", kernel.On("connection"), kernel.Effect("ConnectionAdvance"), kernel.Needs("accounting_sync"))
	r.Command("account.upsert_from_accounting", kernel.On("account"), kernel.Rootless(), kernel.Effect("AccountUpsertFromAccounting"),
		kernel.Needs("accounting_sync"))
	r.Command("project.upsert_from_accounting", kernel.On("project"), kernel.Rootless(), kernel.Effect("ProjectUpsertFromAccounting"),
		kernel.Needs("accounting_sync"))

	r.Screen(kernel.Screen{ID: "accounting", Group: "system", Icon: "arrow-sync", Views: []string{"connection"}, Table: "connection",
		Commands: []string{"connection.pull"},
		Children: []kernel.Screen{{ID: "connection_delivery", Views: []string{"connection_delivery"}, Commands: []string{"delivery.retry"},
			Link: map[string]string{"connection_id": "id"}}}})
}}
