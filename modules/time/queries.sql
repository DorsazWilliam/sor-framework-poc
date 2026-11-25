-- name: Timesheet :many
-- Activities of a user over a period, with their project, folder and task, by date and start.
-- @anchor user_id user
select a.*, p.name as project_name, f.name as folder_name, t.name as task_name, t.billing
from activity_p a
left join project p on p.id = a.project_id
left join folder f on f.id = a.folder_id
left join task t on t.id = a.task_id
where a.user_id = @user_id and a.date between @date_from and @date_to and a.user_id = any(@reach::bigint[])
order by a.date, a.start_tod, a.id;

-- name: TimesheetDay :many
-- Days of a user over a period: minutes due and counted, public holiday, whether the day is locked.
-- @anchor user_id user
select @user_id::bigint as user_id, d.date, round(w.due_min)::integer as due_min, coalesce(s.done_min, 0)::integer as done_min,
       h.label as holiday, coalesce(d.date <@ l.dates, false) as locked
from generate_series(@date_from::date, @date_to::date, interval '1 day') g
cross join lateral (select g::date as date) d
cross join lateral work_due(@user_id::bigint, d.date, d.date) w
left join (
    select a.date, sum(a.counted_min) as done_min
    from activity_p a
    where a.user_id = @user_id::bigint and a.date between @date_from::date and @date_to::date
    group by a.date
) s on s.date = d.date
left join public_holiday h on h.date = d.date
left join lock_user l on l.user_id = @user_id::bigint
where @user_id::bigint = any(@reach::bigint[])
order by d.date;

-- name: ActivityGrid :many
-- Activities in reach over a period, with user, project, folder and task names.
-- @anchor user_id user
select a.*, u.name as user_name, p.name as project_name, f.name as folder_name, t.name as task_name
from activity_p a
left join "user" u on u.id = a.user_id
left join project p on p.id = a.project_id
left join folder f on f.id = a.folder_id
left join task t on t.id = a.task_id
where a.date between @date_from and @date_to and a.user_id = any(@reach::bigint[])
order by a.date desc, a.id desc;

-- name: ActivityToValidate :many
-- Activities of the team over a period that need a validation and are not validated.
-- @anchor user_id team
-- @needs validation
select a.*, u.name as user_name, p.name as project_name, f.name as folder_name, t.name as task_name
from activity_p a
left join "user" u on u.id = a.user_id
left join project p on p.id = a.project_id
left join folder f on f.id = a.folder_id
left join task t on t.id = a.task_id
where a.date between @date_from and @date_to and a.to_validate and a.not_validated and a.user_id = any(@reach::bigint[])
order by a.date desc, a.id desc;

-- name: ActivityClass :many
-- Split of an activity by hour class, plus the part outside any class.
-- @anchor user_id user
select c.*, h.label
from activity_class c
left join hour_class h on h.id = c.hour_class_id
where c.activity_id = @activity_id and c.user_id = any(@reach::bigint[])
order by h.label nulls first;

-- name: ExpenseByActivity :many
-- Expense lines of an activity with their amounts.
-- @anchor user_id user
select e.*, t.name as type_name, t.by_quantity
from expense_p e
left join expense_type t on t.id = e.expense_type_id
where e.activity_id = @activity_id and e.user_id = any(@reach::bigint[])
order by e.id;

-- name: Balance :many
-- Hours and vacation balances per user in reach over [date_from, date_to], nothing after today, in minutes.
-- Carried = done minus due from the first work period of the user to date_from excluded.
-- @anchor user_id user
with r as (
    select @date_from::date as a, least(@date_to::date, sor_today()) as b
), ws as (
    select w.user_id, min(w.valid_from) as first_day from work_period w group by w.user_id
), d as (
    select x.user_id,
           coalesce(sum(x.counted_min) filter (where x.date < r.a and x.date >= ws.first_day), 0) as done_before,
           coalesce(sum(x.counted_min) filter (where x.date >= r.a), 0) as done,
           coalesce(sum(x.duration_min) filter (where x.kind = 'vacation' and x.date < r.a and x.date >= ws.first_day), 0) as vac_before,
           coalesce(sum(x.duration_min) filter (where x.kind = 'vacation' and x.date >= r.a), 0) as vac_taken
    from activity_p x
    cross join r
    left join ws on ws.user_id = x.user_id
    where x.date <= r.b and x.user_id = any(@reach::bigint[])
    group by x.user_id
)
select u.id as user_id, k.carried_min, k.due_min, k.done_min, k.carried_min + k.done_min - k.due_min as balance_min,
       k.vac_carried_min, k.vac_available_min, k.vac_taken_min, k.vac_carried_min + k.vac_available_min - k.vac_taken_min as vac_balance_min
from "user" u
cross join r
left join ws on ws.user_id = u.id
left join d on d.user_id = u.id
cross join lateral work_due(u.id, ws.first_day, r.a - 1) w0
cross join lateral work_due(u.id, r.a, r.b) w1
cross join lateral (
    select round(coalesce(d.done_before, 0) - w0.due_min)::integer as carried_min,
           round(w1.due_min)::integer as due_min,
           coalesce(d.done, 0)::integer as done_min,
           round(w0.vacation_min - coalesce(d.vac_before, 0))::integer as vac_carried_min,
           round(w1.vacation_min)::integer as vac_available_min,
           coalesce(d.vac_taken, 0)::integer as vac_taken_min
) k
where u.kind = 'human' and u.id = any(@reach::bigint[])
order by u.name;

-- name: ActivityValidate :exec
-- Marks activities validated by the principal, now.
update activity set validated_by = @self, validated_at = sor_now() where id = any(@ids::bigint[]);

-- name: ActivityUnvalidate :exec
-- Removes the validation of activities.
update activity set validated_by = null, validated_at = null where id = any(@ids::bigint[]);

-- name: ExpenseAttachReceipt :exec
-- Attaches a receipt file to expenses.
update expense set receipt_file_id = @receipt_file_id where id = any(@ids::bigint[]);

-- name: ExpenseApplyOcr :exec
-- Stores the amount read on the receipt; it becomes the amount when none was entered.
update expense set ocr_amount = @ocr_amount::numeric, amount = coalesce(amount, @ocr_amount::numeric) where id = any(@ids::bigint[]);
