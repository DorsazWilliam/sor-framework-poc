// Package base declares the company, the principals, their groups and the master data.
package base

import (
	_ "embed"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

//go:embed schema.sql
var schema string

// Module is the base module.
var Module = kernel.Module{Name: "base", Schema: schema, Register: func(r *kernel.Registry) {
	md := kernel.Write("master_data")
	// what time entry needs is read by any principal; groups and rates by master data; costs by cost
	anyone, mdOnly := kernel.Read(kernel.Any), kernel.Read("master_data")
	group := kernel.Group("master_data")
	r.Table("file", mdOnly, md)
	r.Table("company", anyone, md, group, kernel.Icon("building-bank"))
	r.Table("user", anyone, md, group, kernel.Icon("person"))
	r.Table("user_group", mdOnly, md, group, kernel.Icon("people-team"))
	r.Table("user_group_member", kernel.ChildOf("user_group", "user_group_id"), mdOnly, md, kernel.Label("user_id"))
	r.Table("user_group_fn", kernel.ChildOf("user_group", "user_group_id"), mdOnly, md, kernel.Label("fn"))
	r.Table("account", anyone, md, group, kernel.Icon("building"))
	r.Table("project", anyone, md, group, kernel.Icon("briefcase"))
	r.Table("folder", anyone, md, group, kernel.Icon("folder"))
	r.Table("folder_task", kernel.ChildOf("folder", "folder_id"), anyone, md, kernel.Label("task_id"))
	r.Table("task", anyone, md, group, kernel.Icon("tag"))
	r.Table("user_task_rate", mdOnly, md, group, kernel.Label("rate"), kernel.Icon("person-money"))
	r.Table("work_period", kernel.Anchor("user_id", "user"), anyone, md, group, kernel.Label("valid_from"), kernel.Icon("calendar-clock"))
	r.Table("cost_rate", kernel.Read("cost"), kernel.Write("cost"), group, kernel.Label("valid_from"), kernel.Icon("money-calculator"))
	r.Table("public_holiday", anyone, md, group, kernel.Label("label"), kernel.Icon("calendar-star"))
	r.Table("hour_class", anyone, md, group, kernel.Label("label"), kernel.Icon("weather-moon"))
	r.Ref("principal_id", "user")
	r.Ref("for_id", "user")
	r.Ref("owner_id", "user")
	r.Table("hour_class_slot", kernel.ChildOf("hour_class", "hour_class_id"), anyone, md, kernel.Label("span_tod"))
}}
