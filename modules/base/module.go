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
	group := kernel.Group("master_data")
	r.Table("company", md, group)
	r.Table("user", md, group)
	r.Table("user_group", md, group)
	r.Table("user_group_member", kernel.ChildOf("user_group", "user_group_id"), md, kernel.Label("user_id"))
	r.Table("user_group_fn", kernel.ChildOf("user_group", "user_group_id"), md, kernel.Label("fn"))
	r.Table("account", md, group)
	r.Table("project", md, group)
	r.Table("folder", md, group)
	r.Table("folder_task", kernel.ChildOf("folder", "folder_id"), md, kernel.Label("task_id"))
	r.Table("task", md, group)
	r.Table("user_task_rate", md, group, kernel.Label("rate"))
	r.Table("work_period", kernel.Anchor("user_id", "user"), md, group, kernel.Label("valid_from"))
	r.Table("cost_rate", kernel.Read("cost"), kernel.Write("cost"), group, kernel.Label("valid_from"))
	r.Table("public_holiday", md, group, kernel.Label("label"))
	r.Table("hour_class", md, group, kernel.Label("label"))
	r.Ref("principal_id", "user")
	r.Ref("for_id", "user")
	r.Ref("owner_id", "user")
	r.Table("hour_class_slot", kernel.ChildOf("hour_class", "hour_class_id"), md, kernel.Label("span_tod"))
}}
