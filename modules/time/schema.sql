create type expense_mode as enum ('both', 'user_only', 'billing_only');
comment on type expense_mode is 'Who an expense concerns: reimbursed to the user and billed, user only, or billing only.';

create table activity (
    id bigint generated always as identity primary key,
    user_id bigint not null references "user",
    date date not null,
    start_tod smallint not null check (start_tod between 0 and 1439),
    duration_min smallint not null check (duration_min > 0),
    billed_min smallint check (billed_min >= 0),
    folder_id bigint not null references folder,
    task_id bigint not null references task,
    description text not null default '',
    validated_by bigint references "user",
    validated_at timestamptz,
    invoice_id bigint,
    frozen_rate numeric(10,2),
    frozen_billed_min smallint,
    frozen_amount numeric(12,2),
    constraint activity_end check (start_tod + duration_min <= 1440),
    constraint activity_validation check ((validated_by is null) = (validated_at is null)),
    constraint activity_frozen check (invoice_id is not null or (frozen_rate is null and frozen_billed_min is null and frozen_amount is null))
);
comment on table activity is 'Time spent by a user on a task in a folder, on a date.';
comment on column activity.user_id is 'User who did the work.';
comment on column activity.date is 'Date of the work.';
comment on column activity.start_tod is 'Start, minute of the day.';
comment on column activity.duration_min is 'Real duration.';
comment on column activity.billed_min is 'Billed duration entered, for a task billed sometimes; empty = the real duration.';
comment on column activity.folder_id is 'Folder worked on.';
comment on column activity.task_id is 'Task done.';
comment on column activity.description is 'What was done.';
comment on column activity.validated_by is 'Manager who validated the activity.';
comment on column activity.validated_at is 'When the activity was validated.';
comment on column activity.invoice_id is 'Invoice the activity is billed on; set when the invoice is marked invoiced.';
comment on column activity.frozen_rate is 'Rate frozen when invoiced.';
comment on column activity.frozen_billed_min is 'Billed duration frozen when invoiced.';
comment on column activity.frozen_amount is 'Amount frozen when invoiced.';
create index activity_user_date on activity (user_id, date) include (start_tod, duration_min, task_id);
create index activity_date on activity (date, id);
create index activity_folder on activity (folder_id, date) include (duration_min);
create index activity_invoice on activity (invoice_id) where invoice_id is not null;

create table expense_type (
    id bigint generated always as identity primary key,
    name text not null,
    by_quantity boolean not null default false,
    unit_price numeric(10,2) check (unit_price >= 0),
    billing_unit_price numeric(10,2) check (billing_unit_price >= 0),
    mode expense_mode not null default 'both',
    archived boolean not null default false,
    constraint expense_type_prices check (not by_quantity or (unit_price is not null and billing_unit_price is not null))
);
comment on table expense_type is 'A type of expense, entered by amount or by quantity.';
comment on column expense_type.name is 'Name of the type.';
comment on column expense_type.by_quantity is 'Entered as a quantity priced by the type, instead of an amount.';
comment on column expense_type.unit_price is 'Price of one unit reimbursed to the user.';
comment on column expense_type.billing_unit_price is 'Price of one unit billed to the client.';
comment on column expense_type.mode is 'Default mode of its expenses.';
comment on column expense_type.archived is 'Archived types are hidden from choices.';

create table expense (
    id bigint generated always as identity primary key,
    activity_id bigint not null references activity,
    expense_type_id bigint not null references expense_type,
    quantity numeric(10,2) check (quantity >= 0),
    amount numeric(10,2) check (amount >= 0),
    mode expense_mode,
    receipt_file_id bigint references file,
    ocr_amount numeric(10,2),
    frozen_user_amount numeric(10,2),
    frozen_billed_amount numeric(10,2)
);
comment on table expense is 'An expense line of an activity.';
comment on column expense.activity_id is 'Activity the expense belongs to.';
comment on column expense.expense_type_id is 'Type of expense.';
comment on column expense.quantity is 'Quantity, for a type by quantity.';
comment on column expense.amount is 'Amount paid, for a type by amount.';
comment on column expense.mode is 'Mode of the expense; empty = the mode of its type.';
comment on column expense.receipt_file_id is 'Receipt (scan or photo).';
comment on column expense.ocr_amount is 'Amount read on the receipt by OCR.';
comment on column expense.frozen_user_amount is 'Amount reimbursed to the user, frozen when invoiced.';
comment on column expense.frozen_billed_amount is 'Amount billed, frozen when invoiced.';
create index expense_activity on expense (activity_id);

create table period_lock (
    id bigint generated always as identity primary key,
    company_id bigint not null default 1 references company,
    date_from date not null,
    date_to date not null,
    user_id bigint references "user",
    constraint period_lock_dates check (date_from <= date_to)
);
comment on table period_lock is 'A period in which no activity or expense can be written, for one user or all.';
comment on column period_lock.company_id is 'Company (always 1); locking a period locks the company row.';
comment on column period_lock.date_from is 'First day locked.';
comment on column period_lock.date_to is 'Last day locked.';
comment on column period_lock.user_id is 'User locked; empty = all users.';

create view lock_user as
select u.id as user_id, range_agg(daterange(l.date_from, l.date_to, '[]')) as dates
from "user" u
join period_lock l on l.user_id is null or l.user_id = u.id
group by u.id;
comment on view lock_user is 'Dates locked for each user: the locks of all users and the user''s own.';
comment on column lock_user.dates is 'Locked dates.';

create view folder_spent as
select f.id as folder_id, f.estimated_min,
       (select coalesce(sum(b.duration_min), 0) from activity b where b.folder_id = f.id) as spent_min
from folder f
where f.estimated_min is not null;
comment on view folder_spent is 'Real duration spent in each folder having an estimate.';
comment on column folder_spent.spent_min is 'Sum of the real durations of the folder.';

create view activity_p as
select q.id, q.user_id, q.date, q.start_tod, q.duration_min, q.folder_id, q.task_id, q.description,
       q.validated_by, q.validated_at, q.invoice_id, q.frozen_rate, q.frozen_billed_min, q.frozen_amount,
       q.project_id, q.day_type, q.kind, q.billed_min, q.rate,
       coalesce(q.frozen_amount, round(q.rate * q.billed_min / 60.0
           * case when q.span && dp.classed then (q.duration_min + hour_class_extra(q.span, dp.slots, dp.rate_extra)) / q.duration_min else 1 end, 2)) as amount,
       case when not q.counts_hours then 0
            when q.span && dp.classed then round(q.duration_min + hour_class_extra(q.span, dp.slots, dp.hours_extra))::integer
            else q.duration_min::integer end as counted_min,
       q.requires_validation or coalesce(q.span && dp.validating, false) or coalesce(fs.spent_min > fs.estimated_min, false) as to_validate,
       q.not_locked, q.not_invoiced, q.not_validated, q.is_validated, q.entry_open, q.task_allowed
from (
    select a.id, a.user_id, a.date, a.start_tod, a.duration_min, a.folder_id, a.task_id, a.description,
           a.validated_by, a.validated_at, a.invoice_id, a.frozen_rate, a.frozen_billed_min, a.frozen_amount,
           f.project_id, f.entry_open, t.kind, t.counts_hours, t.requires_validation,
           case when h.id is not null then 8 else extract(isodow from a.date)::integer end as day_type,
           int4range(a.start_tod, a.start_tod + a.duration_min) as span,
           coalesce(a.frozen_billed_min, case t.billing when 'always' then a.duration_min
                                                        when 'sometimes' then coalesce(a.billed_min, a.duration_min) else 0 end) as billed_min,
           coalesce(a.frozen_rate, ur.rate, f.rate, p.rate, t.rate, 0) as rate,
           not coalesce(a.date <@ lu.dates, false) as not_locked,
           a.invoice_id is null as not_invoiced,
           a.validated_at is null as not_validated,
           a.validated_at is not null as is_validated,
           ft.id is not null as task_allowed
    from activity a
    left join task t on t.id = a.task_id
    left join folder_p f on f.id = a.folder_id
    left join project p on p.id = f.project_id
    left join user_task_rate ur on ur.user_id = a.user_id and ur.task_id = a.task_id
    left join public_holiday h on h.date = a.date and h.deducted_min is null
    left join folder_task ft on ft.folder_id = a.folder_id and ft.task_id = a.task_id
    left join lock_user lu on lu.user_id = a.user_id
) q
left join day_profile dp on dp.day_type = q.day_type
left join folder_spent fs on fs.folder_id = q.folder_id;
comment on view activity_p is 'Projection of activity: rates, amounts, counted minutes and conditions, computed; frozen values once invoiced.';
comment on column activity_p.project_id is 'Project of the folder.';
comment on column activity_p.day_type is 'ISO weekday 1 to 7, or 8 on a full public holiday.';
comment on column activity_p.kind is 'Kind of the task.';
comment on column activity_p.billed_min is 'Billed duration: frozen, else the real duration (always), the entered one or the real one (sometimes), 0 (never).';
comment on column activity_p.rate is 'Hourly rate: frozen, else user and task, folder, project, task, 0.';
comment on column activity_p.amount is 'Amount: frozen, else rate times billed hours, weighted by the rate coefficients of the hour classes touched.';
comment on column activity_p.counted_min is 'Minutes counted in the hours done: 0 for a task not counting hours, else weighted by the hours coefficients of the hour classes touched.';
comment on column activity_p.to_validate is 'Needs a manager validation: the task, an hour class touched, or the folder estimate exceeded.';
comment on column activity_p.not_locked is 'The date is in no locked period of the user or of all.';
comment on column activity_p.not_invoiced is 'Not on an invoiced invoice.';
comment on column activity_p.not_validated is 'Not validated.';
comment on column activity_p.is_validated is 'Validated.';
comment on column activity_p.entry_open is 'The folder and its project accept activities.';
comment on column activity_p.task_allowed is 'The task is allowed in the folder.';

create view activity_class as
select p.id as activity_id, p.user_id, o.hour_class_id, o.minutes,
       case when t.counts_hours then o.minutes * coalesce(c.hours_coef, 1) else 0 end as counted_min,
       round(p.rate * p.billed_min / 60.0 * o.minutes * coalesce(c.rate_coef, 1) / p.duration_min, 2) as amount
from activity_p p
join (
    select a.id as activity_id, s.hour_class_id,
           sum(upper(s.span_tod * int4range(a.start_tod, a.start_tod + a.duration_min))
               - lower(s.span_tod * int4range(a.start_tod, a.start_tod + a.duration_min)))::integer as minutes
    from activity_p a
    join hour_class_slot s on s.day_type = a.day_type and s.span_tod && int4range(a.start_tod, a.start_tod + a.duration_min)
    group by a.id, s.hour_class_id
    union all
    select a.id, null::bigint,
           (a.duration_min - coalesce((select sum(upper(s.span_tod * int4range(a.start_tod, a.start_tod + a.duration_min))
                                                  - lower(s.span_tod * int4range(a.start_tod, a.start_tod + a.duration_min)))
                                       from hour_class_slot s
                                       where s.day_type = a.day_type and s.span_tod && int4range(a.start_tod, a.start_tod + a.duration_min)), 0))::integer
    from activity_p a
) o on o.activity_id = p.id and o.minutes > 0
left join task t on t.id = p.task_id
left join hour_class c on c.id = o.hour_class_id;
comment on view activity_class is 'Split of each activity by hour class touched, plus the part outside any class.';
comment on column activity_class.activity_id is 'Activity.';
comment on column activity_class.hour_class_id is 'Hour class; empty = outside any class.';
comment on column activity_class.minutes is 'Real minutes in the class.';
comment on column activity_class.counted_min is 'Minutes counted, weighted by the hours coefficient.';
comment on column activity_class.amount is 'Share of the amount, weighted by the rate coefficient.';

create view expense_p as
select q.id, q.activity_id, q.expense_type_id, q.quantity, q.amount, q.mode, q.receipt_file_id, q.ocr_amount,
       q.frozen_user_amount, q.frozen_billed_amount, q.user_id, q.effective_mode,
       coalesce(q.frozen_user_amount, case when q.effective_mode = 'billing_only' then 0
                                           when q.by_quantity then round(q.quantity * q.unit_price, 2)
                                           else coalesce(q.amount, 0) end) as user_amount,
       coalesce(q.frozen_billed_amount, case when q.effective_mode = 'user_only' then 0
                                             when q.by_quantity then round(q.quantity * q.billing_unit_price, 2)
                                             else coalesce(q.amount, 0) end) as billed_amount
from (
    select e.*, a.user_id, t.by_quantity, t.unit_price, t.billing_unit_price, coalesce(e.mode, t.mode) as effective_mode
    from expense e
    left join activity a on a.id = e.activity_id
    left join expense_type t on t.id = e.expense_type_id
) q;
comment on view expense_p is 'Projection of expense: effective mode and amounts reimbursed and billed; frozen values once invoiced.';
comment on column expense_p.user_id is 'User of the activity.';
comment on column expense_p.effective_mode is 'Mode of the expense, else of its type.';
comment on column expense_p.user_amount is 'Amount reimbursed to the user: frozen, else 0 (billing only), quantity times unit price, or the amount.';
comment on column expense_p.billed_amount is 'Amount billed: frozen, else 0 (user only), quantity times billing unit price, or the amount.';
