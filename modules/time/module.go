// Package time declares activities, expenses, validation, period locks and balances.
package time

import (
	_ "embed"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:embed schema.sql
var schema string

var (
	// activityTotals are the sums of an activity list.
	activityTotals = kernel.Sum("duration_min", "billed_min", "counted_min", "amount")
	expenses       = kernel.Screen{ID: "activity_expense", Views: []string{"expense_by_activity"}, Table: "expense",
		Commands: []string{"expense.attach_receipt"}, Link: map[string]string{"activity_id": "id"}}
	classes = kernel.Screen{ID: "activity_class", Views: []string{"activity_class"}, Link: map[string]string{"activity_id": "id"}}
)

// Module is the time module.
var Module = kernel.Module{Name: "time", Schema: schema, Register: func(r *kernel.Registry) {
	r.Table("activity", kernel.Under("company"), kernel.Anchor("user_id", "user"), kernel.Read("activity"), kernel.Write("activity"),
		kernel.Guarded("validated_by", "validated_at"), kernel.Label("description"))
	r.Table("expense_type", kernel.Read(kernel.Any), kernel.Write("master_data"), kernel.Group("master_data"))
	r.Table("expense", kernel.ChildOf("activity", "activity_id"), kernel.Read("expense"), kernel.Write("expense"),
		kernel.Guarded("receipt_file_id", "ocr_amount"), kernel.Label("expense_type_id"))
	r.Table("period_lock", kernel.ChildOf("company", "company_id"), kernel.Read("period_lock"), kernel.Write("period_lock"), kernel.Label("date_from"))

	r.Action("activity", "editable", "not_locked", "not_invoiced", "not_validated", "entry_open", "task_allowed")
	r.Action("activity", "deletable", "not_locked", "not_invoiced", "not_validated")
	r.Action("activity", "validatable", "to_validate", "not_validated")
	r.Action("activity", "unvalidatable", "is_validated", "not_invoiced")

	r.Command("activity.validate", kernel.On("activity"), kernel.When("validatable"), kernel.Effect("ActivityValidate"),
		kernel.Needs("validation"), kernel.Reach("team"))
	r.Command("activity.unvalidate", kernel.On("activity"), kernel.When("unvalidatable"), kernel.Effect("ActivityUnvalidate"),
		kernel.Needs("validation"), kernel.Reach("team"))
	r.Command("expense.attach_receipt", kernel.On("expense"), kernel.When("editable"), kernel.Ensure("editable"),
		kernel.Effect("ExpenseAttachReceipt"), kernel.Needs("expense"))
	r.Command("expense.apply_ocr", kernel.On("expense"), kernel.When("editable"), kernel.Effect("ExpenseApplyOcr"), kernel.Needs("ocr"))
	r.Job("ocr.read", ocrRead, kernel.As("svc-ocr"))
	r.Reaction("expense.attach_receipt", "ocr.read")

	r.Screen(kernel.Screen{ID: "entry", Group: "time", Component: "entry", Views: []string{"timesheet", "timesheet_day"},
		Defaults: map[string]string{"user_id": "$self", "date_from": "$today", "date_to": "$today"}, Table: "activity",
		Commands: []string{"activity.create", "activity.update", "activity.delete"}, Children: []kernel.Screen{expenses, classes}})
	r.Screen(kernel.Screen{ID: "validation", Group: "time", Views: []string{"activity_to_validate"},
		Defaults: map[string]string{"date_from": "$days(-90)", "date_to": "$today"}, Table: "activity",
		Commands: []string{"activity.validate"},
		Wrap:     &kernel.Wrap{Sort: []kernel.Sort{{Col: "date", Dir: "desc"}, {Col: "id", Dir: "desc"}}, Agg: activityTotals},
		Children: []kernel.Screen{expenses, classes}})
	r.Screen(kernel.Screen{ID: "activities", Group: "time", Views: []string{"activity_grid"},
		Defaults: map[string]string{"date_from": "$month_start", "date_to": "$today"}, Table: "activity",
		Commands: []string{"activity.validate", "activity.unvalidate"},
		Wrap:     &kernel.Wrap{Sort: []kernel.Sort{{Col: "date", Dir: "desc"}, {Col: "id", Dir: "desc"}}, Agg: activityTotals},
		Children: []kernel.Screen{expenses, classes}})
	r.Screen(kernel.Screen{ID: "balances", Group: "time", Views: []string{"balance"},
		Defaults: map[string]string{"date_from": "$year_start", "date_to": "$today"},
		Wrap: &kernel.Wrap{Agg: kernel.Sum("carried_min", "due_min", "done_min", "balance_min",
			"vac_carried_min", "vac_available_min", "vac_taken_min", "vac_balance_min")}})
	r.Screen(kernel.Screen{ID: "locks", Group: "time", Views: []string{"period_lock"}, Table: "period_lock",
		Wrap: &kernel.Wrap{Sort: []kernel.Sort{{Col: "date_from", Dir: "desc"}}}})
}}
