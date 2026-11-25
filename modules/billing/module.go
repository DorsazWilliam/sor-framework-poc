// Package billing declares invoices, their invoicing run and the profitability views.
package billing

import (
	_ "embed"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:embed schema.sql
var schema string

// Template is the Typst template of the invoice PDF; it reads the invoice document from data.json.
//
//go:embed invoice.typ
var Template string

// Module is the billing module.
var Module = kernel.Module{Name: "billing", Schema: schema, Register: func(r *kernel.Registry) {
	r.Table("invoice", kernel.Write("invoice"), kernel.Guarded("invoiced_at", "invoiced_by", "pdf_file_id"))

	r.Action("invoice", "editable", "is_draft")
	r.Action("invoice", "deletable", "is_draft")
	r.Action("invoice", "invoiceable", "is_draft", "has_activities")
	r.Action("invoice", "billed", "is_invoiced", "has_activities")
	r.Action("invoice", "revertible", "is_invoiced", "not_exported")
	r.Action("invoice", "exportable", "is_invoiced", "not_exported")

	r.Command("invoice.mark_invoiced", kernel.On("invoice"), kernel.When("invoiceable"), kernel.Ensure("billed"), kernel.Needs("invoice_state"),
		kernel.Effect("InvoiceLockActivities", "InvoiceAttachActivities", "InvoiceFreezeActivities", "InvoiceFreezeExpenses", "InvoiceMarkInvoiced"))
	r.Command("invoice.back_to_draft", kernel.On("invoice"), kernel.When("revertible"), kernel.Needs("invoice_state"),
		kernel.Effect("InvoiceLockActivities", "InvoiceReleaseExpenses", "InvoiceReleaseActivities", "InvoiceBackToDraft"))
	r.Command("invoice.print", kernel.On("invoice"), kernel.Needs("invoice"))
	r.Job("invoice.render", invoiceRender)
	r.Reaction("invoice.print", "invoice.render")
	r.Command("invoice.attach_pdf", kernel.On("invoice"), kernel.Effect("InvoiceAttachPdf"), kernel.Needs("invoice"))
	r.Run("invoice.run", "invoice_run_candidates", "invoice.create", kernel.Needs("invoice"))
	r.Command("delivery.retry", kernel.Needs("invoice"))

	sum := func(cols ...string) []kernel.Agg {
		aggs := make([]kernel.Agg, len(cols))
		for i, c := range cols {
			aggs[i] = kernel.Agg{Col: c, Fn: "sum"}
		}
		return aggs
	}
	r.Screen(kernel.Screen{ID: "profitability", Group: "analysis", Views: []string{"profitability_cost", "profitability"},
		Defaults: map[string]string{"date_from": "$year_start", "date_to": "$today"},
		Wrap: &kernel.Wrap{Group: []string{"project_id"}, Agg: sum("duration_min", "billed_min", "services_amount", "expenses_billed",
			"expenses_user", "realized_amount", "invoiced_amount", "balance_amount")}})
	r.Screen(kernel.Screen{ID: "invoicing", Group: "billing", Views: []string{"invoice"}, Table: "invoice",
		Commands: []string{"invoice.run", "invoice.mark_invoiced", "invoice.back_to_draft", "invoice.print", "invoice.export"},
		Wrap:     &kernel.Wrap{Sort: []kernel.Sort{{Col: "id", Dir: "desc"}}},
		Children: []kernel.Screen{
			{ID: "invoice_activity", Views: []string{"invoice_activity"}, Link: map[string]string{"invoice_id": "id"}},
			{ID: "invoice_export", Views: []string{"accounting_export"}, Commands: []string{"delivery.retry"}, Link: map[string]string{"invoice_id": "id"}},
			{ID: "invoice_pdf", Component: "pdf", Views: []string{"invoice"}, Link: map[string]string{"id": "id"}},
		}})
}}
