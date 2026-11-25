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
			Link: map[string]string{"connection_id": "id"}, Wrap: &kernel.Wrap{Sort: []kernel.Sort{{Col: "id", Dir: "desc"}}}}}})

	// The default dashboard reads views of every module: it is declared here, by the module that comes last.
	count := kernel.Agg{Col: "id", Fn: "count"}
	byPeriod := func(col, by string) []kernel.GroupBy { return []kernel.GroupBy{{Col: col, By: by}} }
	top := func(group, col string) *kernel.Wrap {
		return &kernel.Wrap{Group: []kernel.GroupBy{{Col: group}}, Agg: kernel.Sum(col), Sort: []kernel.Sort{{Col: "sum_" + col, Dir: "desc"}}, Limit: 10}
	}
	r.Screen(kernel.Screen{ID: "dashboard", Group: "analysis", Icon: "board", Component: "dashboard",
		Defaults: map[string]string{"date_from": "$year_start", "date_to": "$today", "user_id": "$self"},
		Tiles: []kernel.Tile{
			{Title: "tile.to_validate", View: "activity_team", Form: "number", Span: 1, Link: "validation",
				Wrap: &kernel.Wrap{Filter: []kernel.Filter{{Action: "validatable"}}, Agg: []kernel.Agg{count}}},
			{Title: "tile.draft_invoices", View: "invoice", Form: "number", Span: 1, Link: "invoicing",
				Wrap: &kernel.Wrap{Filter: []kernel.Filter{{Col: "is_draft", Op: "eq", Value: true}}, Agg: append([]kernel.Agg{count}, kernel.Sum("total_amount")...)}},
			{Title: "tile.failed_exports", View: "connection_delivery", Form: "number", Span: 1, Link: "accounting",
				Wrap: &kernel.Wrap{Filter: []kernel.Filter{{Col: "state", Op: "eq", Value: "failed"}}, Agg: []kernel.Agg{count}}},
			{Title: "tile.hours_by_week", View: "timesheet_day", Form: "bar", Span: 2,
				Wrap: &kernel.Wrap{Group: byPeriod("date", "week"), Agg: kernel.Sum("done_min", "due_min"), Sort: []kernel.Sort{{Col: "date_week", Dir: "asc"}}}},
			{Title: "tile.hours_by_kind", View: "activity_grid", Form: "donut", Span: 2,
				Wrap: &kernel.Wrap{Group: []kernel.GroupBy{{Col: "kind"}}, Agg: kernel.Sum("duration_min")}},
			{Title: "tile.hours_by_day", View: "timesheet_day", Form: "heatmap", Span: 4,
				Wrap: &kernel.Wrap{Group: byPeriod("date", "day"), Agg: kernel.Sum("done_min"), Sort: []kernel.Sort{{Col: "date_day", Dir: "asc"}}, Limit: 366}},
			{Title: "tile.revenue_by_month", View: "revenue", Form: "bar", Span: 2,
				Wrap: &kernel.Wrap{Group: byPeriod("date", "month"), Agg: kernel.Sum("realized_amount", "invoiced_amount"), Sort: []kernel.Sort{{Col: "date_month", Dir: "asc"}}}},
			{Title: "tile.unbilled_by_project", View: "profitability", Form: "bar", Horizontal: true, Span: 2, Link: "profitability",
				Wrap: top("project_id", "balance_amount")},
			{Title: "tile.budget_by_folder", View: "folder_budget", Form: "progress", Span: 2,
				Wrap: &kernel.Wrap{Group: []kernel.GroupBy{{Col: "folder_id"}}, Agg: kernel.Sum("spent_min", "estimated_min"),
					Sort: []kernel.Sort{{Col: "sum_spent_min", Dir: "desc"}}, Limit: 10}},
			{Title: "tile.margin_by_project", View: "profitability_cost", Form: "bar", Horizontal: true, Span: 2, Link: "profitability",
				Wrap: top("project_id", "services_margin_amount")},
		}})
}}
