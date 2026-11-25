create type invoice_state as enum ('draft', 'invoiced');
comment on type invoice_state is 'State of an invoice: draft (its activities are candidates) or invoiced (its lines written).';
create type export_state as enum ('none', 'created', 'exported', 'failed');
comment on type export_state is 'State of an export to an external system: none, created (not yet accepted), exported, failed (last attempt).';

create table invoice (
    id bigint generated always as identity primary key,
    project_id bigint not null references project,
    date_from date not null,
    date_to date not null,
    invoiced_at timestamptz,
    invoiced_by bigint references "user",
    pdf_file_id bigint references file,
    constraint invoice_dates check (date_from <= date_to),
    constraint invoice_invoiced check ((invoiced_at is null) = (invoiced_by is null))
);
comment on table invoice is 'An invoice of a project over a period.';
comment on column invoice.project_id is 'Project invoiced; the client is the account of the project.';
comment on column invoice.date_from is 'First day of the period.';
comment on column invoice.date_to is 'Last day of the period.';
comment on column invoice.invoiced_at is 'When the invoice was marked invoiced.';
comment on column invoice.invoiced_by is 'Who marked the invoice invoiced.';
comment on column invoice.pdf_file_id is 'Last PDF rendered.';
create index invoice_project on invoice (project_id, date_from);

create table invoice_line (
    id bigint generated always as identity primary key,
    invoice_id bigint not null references invoice,
    activity_id bigint not null references activity,
    rate numeric(10,2) not null,
    billed_min integer not null,
    amount numeric(12,2) not null,
    constraint invoice_line_one_invoice unique (activity_id)
);
comment on table invoice_line is 'An activity billed on an invoice, with the values of its projection when the invoice was marked invoiced.';
comment on column invoice_line.invoice_id is 'Invoice.';
comment on column invoice_line.activity_id is 'Activity billed; on one invoice at most.';
comment on column invoice_line.rate is 'Hourly rate billed.';
comment on column invoice_line.billed_min is 'Duration billed.';
comment on column invoice_line.amount is 'Amount billed.';
create index invoice_line_invoice on invoice_line (invoice_id);

create table invoice_expense (
    id bigint generated always as identity primary key,
    invoice_id bigint not null references invoice,
    expense_id bigint not null references expense,
    user_amount numeric(10,2) not null,
    billed_amount numeric(10,2) not null,
    constraint invoice_expense_one_invoice unique (expense_id)
);
comment on table invoice_expense is 'An expense billed on an invoice, with the amounts of its projection when the invoice was marked invoiced.';
comment on column invoice_expense.invoice_id is 'Invoice.';
comment on column invoice_expense.expense_id is 'Expense billed; on one invoice at most.';
comment on column invoice_expense.user_amount is 'Amount reimbursed to the user.';
comment on column invoice_expense.billed_amount is 'Amount billed.';
create index invoice_expense_invoice on invoice_expense (invoice_id);

create view billable_activity as
select a.id as activity_id, a.folder_id, a.date
from activity a
where a.task_id = any(array(select t.id from task t where t.billing <> 'never'))
  and not exists (select from invoice_line l where l.activity_id = a.id);
comment on view billable_activity is 'Activities an invoice can take: on no invoice line, their task billed (not never).';
comment on column billable_activity.activity_id is 'Activity.';
comment on column billable_activity.folder_id is 'Folder of the activity.';
comment on column billable_activity.date is 'Date of the activity.';

create view invoice_item as
select l.invoice_id, l.activity_id
from invoice_line l
union all
select i.id, b.activity_id
from invoice i
join folder f on f.project_id = i.project_id
join billable_activity b on b.folder_id = f.id and b.date between i.date_from and i.date_to
where i.invoiced_at is null;
comment on view invoice_item is 'Activities of an invoice: its lines once invoiced; while draft, the billable activities of its project and period.';
comment on column invoice_item.invoice_id is 'Invoice.';
comment on column invoice_item.activity_id is 'Activity.';

create view activity_p as
select q.id, q.user_id, q.date, q.start_tod, q.duration_min, q.folder_id, q.task_id, q.description,
       q.validated_by, q.validated_at, q.invoice_id,
       q.project_id, q.day_type, q.span_tod, q.kind, q.billed_min, q.rate,
       coalesce(q.line_amount, round(q.rate * q.billed_min / 60.0
           * case when q.span_tod && dp.classed then (q.duration_min + hour_class_extra(q.span_tod, dp.slots, dp.rate_extra)) / q.duration_min else 1 end, 2)) as amount,
       case when not q.counts_hours then 0
            when q.span_tod && dp.classed then round(q.duration_min + hour_class_extra(q.span_tod, dp.slots, dp.hours_extra))::integer
            else q.duration_min::integer end as counted_min,
       q.requires_validation or coalesce(q.span_tod && dp.validating, false) or coalesce(fs.spent_min > fs.estimated_min, false) as to_validate,
       q.not_locked, q.not_invoiced, q.not_validated, q.is_validated, q.entry_open, q.task_allowed
from (
    select a.id, a.user_id, a.date, a.start_tod, a.duration_min, a.folder_id, a.task_id, a.description,
           a.validated_by, a.validated_at, l.invoice_id, l.amount as line_amount,
           f.project_id, f.entry_open, t.kind, t.counts_hours, t.requires_validation,
           case when h.id is not null then 8 else extract(isodow from a.date)::integer end as day_type,
           int4range(a.start_tod, a.start_tod + a.duration_min) as span_tod,
           coalesce(l.billed_min, case t.billing when 'always' then a.duration_min
                                                 when 'sometimes' then coalesce(a.billed_min, a.duration_min) else 0 end) as billed_min,
           coalesce(l.rate, ur.rate, f.rate, p.rate, t.rate, 0) as rate,
           not coalesce(a.date <@ lu.dates, false) as not_locked,
           l.id is null as not_invoiced,
           a.validated_at is null as not_validated,
           a.validated_at is not null as is_validated,
           ft.id is not null as task_allowed
    from activity a
    left join invoice_line l on l.activity_id = a.id
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
comment on view activity_p is 'Projection of activity: rates, amounts, counted minutes and conditions, computed; the values of its invoice line once invoiced.';
comment on column activity_p.invoice_id is 'Invoice of its invoice line.';
comment on column activity_p.project_id is 'Project of the folder.';
comment on column activity_p.day_type is 'ISO weekday 1 to 7, or 8 on a full public holiday.';
comment on column activity_p.span_tod is 'Minutes of the day worked: from the start, for the duration.';
comment on column activity_p.kind is 'Kind of the task.';
comment on column activity_p.billed_min is 'Billed duration: that of its invoice line, else the real duration (always), the entered one or the real one (sometimes), 0 (never).';
comment on column activity_p.rate is 'Hourly rate: that of its invoice line, else user and task, folder, project, task, 0.';
comment on column activity_p.amount is 'Amount: that of its invoice line, else rate times billed hours, weighted by the rate coefficients of the hour classes touched.';
comment on column activity_p.counted_min is 'Minutes counted in the hours done: 0 for a task not counting hours, else weighted by the hours coefficients of the hour classes touched.';
comment on column activity_p.to_validate is 'Needs a manager validation: the task, an hour class touched, or the folder estimate exceeded.';
comment on column activity_p.not_locked is 'The date is in no locked period of the user or of all.';
comment on column activity_p.not_invoiced is 'On no invoice line.';
comment on column activity_p.not_validated is 'Not validated.';
comment on column activity_p.is_validated is 'Validated.';
comment on column activity_p.entry_open is 'The folder and its project accept activities.';
comment on column activity_p.task_allowed is 'The task is allowed in the folder.';

create view activity_class as
select q.activity_id, q.user_id, q.hour_class_id, q.minutes as duration_min,
       coalesce(q.counted_min * q.minutes * q.hours_coef / nullif(sum(q.minutes * q.hours_coef) over w, 0), 0) as counted_min,
       coalesce(round(q.amount * q.minutes * q.rate_coef / nullif(sum(q.minutes * q.rate_coef) over w, 0), 2), 0) as amount
from (
    select p.id as activity_id, p.user_id, p.counted_min, p.amount, o.hour_class_id,
           case when o.hour_class_id is null then p.duration_min - coalesce(o.minutes, 0) else o.minutes end as minutes,
           coalesce(c.hours_coef, 1) as hours_coef, coalesce(c.rate_coef, 1) as rate_coef
    from activity_p p
    cross join lateral (
        select s.hour_class_id, sum(upper(s.span_tod * p.span_tod) - lower(s.span_tod * p.span_tod))::integer as minutes
        from hour_class_slot s
        where s.day_type = p.day_type and s.span_tod && p.span_tod
        group by rollup (s.hour_class_id)
    ) o
    left join hour_class c on c.id = o.hour_class_id
) q
where q.minutes > 0
window w as (partition by q.activity_id);
comment on view activity_class is 'Split of each activity by hour class touched, plus the part outside any class: its minutes, and its share of the counted minutes and of the amount of the activity, weighted by the coefficients of the class.';
comment on column activity_class.activity_id is 'Activity.';
comment on column activity_class.hour_class_id is 'Hour class; empty = outside any class.';
comment on column activity_class.duration_min is 'Real minutes in the class.';
comment on column activity_class.counted_min is 'Minutes counted, weighted by the hours coefficient.';
comment on column activity_class.amount is 'Share of the amount, weighted by the rate coefficient.';

create view expense_p as
select q.id, q.activity_id, q.expense_type_id, q.quantity, q.amount, q.mode, q.receipt_file_id, q.ocr_amount, q.user_id, q.effective_mode,
       coalesce(q.line_user_amount, case when q.effective_mode = 'billing_only' then 0
                                         when q.by_quantity then round(q.quantity * q.unit_price, 2)
                                         else coalesce(q.amount, 0) end) as user_amount,
       coalesce(q.line_billed_amount, case when q.effective_mode = 'user_only' then 0
                                           when q.by_quantity then round(q.quantity * q.billing_unit_price, 2)
                                           else coalesce(q.amount, 0) end) as billed_amount
from (
    select e.*, l.user_amount as line_user_amount, l.billed_amount as line_billed_amount,
           a.user_id, t.by_quantity, t.unit_price, t.billing_unit_price, coalesce(e.mode, t.mode) as effective_mode
    from expense e
    left join invoice_expense l on l.expense_id = e.id
    left join activity a on a.id = e.activity_id
    left join expense_type t on t.id = e.expense_type_id
) q;
comment on view expense_p is 'Projection of expense: effective mode and amounts reimbursed and billed; the amounts of its invoice line once invoiced.';
comment on column expense_p.user_id is 'User of the activity.';
comment on column expense_p.effective_mode is 'Mode of the expense, else of its type.';
comment on column expense_p.user_amount is 'Amount reimbursed to the user: that of its invoice line, else 0 (billing only), quantity times unit price, or the amount.';
comment on column expense_p.billed_amount is 'Amount billed: that of its invoice line, else 0 (user only), quantity times billing unit price, or the amount.';

create view delivery_state as
select q.delivery_id, q.connection_id, q.root_table, q.root_id, q.created_at,
       (case when q.ok then 'exported' when q.last_at is not null then 'failed' else 'created' end)::export_state as state,
       q.attempts, q.last_at, q.last_status, q.last_detail
from (
    select d.id as delivery_id, d.connection_id, d.root_table, d.root_id, d.created_at,
           (select count(*) from delivery_result x where x.delivery_id = d.id) as attempts,
           exists (select from delivery_result x where x.delivery_id = d.id and x.ok) as ok,
           (select max(x.at) from delivery_result x where x.delivery_id = d.id) as last_at,
           (select x.status from delivery_result x where x.delivery_id = d.id order by x.at desc, x.id desc limit 1) as last_status,
           (select x.detail from delivery_result x where x.delivery_id = d.id order by x.at desc, x.id desc limit 1) as last_detail
    from delivery d
) q;
comment on view delivery_state is 'State of each delivery, from its results.';
comment on column delivery_state.state is 'Exported once accepted; failed if the last attempt failed; created before any attempt.';
comment on column delivery_state.attempts is 'Number of attempts.';
comment on column delivery_state.last_at is 'Instant of the last attempt.';
comment on column delivery_state.last_status is 'HTTP status of the last attempt.';
comment on column delivery_state.last_detail is 'Detail of the last attempt.';

create view invoice_p as
select q.id, q.project_id, q.date_from, q.date_to, q.invoiced_at, q.invoiced_by, q.pdf_file_id, q.account_id,
       (case when q.invoiced_at is null then 'draft' else 'invoiced' end)::invoice_state as state,
       q.activity_count, q.billed_min, q.services_amount, q.expenses_amount,
       q.services_amount + q.expenses_amount as total_amount,
       coalesce(q.last_export, 'none') as export_state,
       q.invoiced_at is null as is_draft,
       q.invoiced_at is not null as is_invoiced,
       q.activity_count > 0 as has_activities,
       q.last_export is null as not_exported
from (
    select i.*, p.account_id, t.activity_count, t.billed_min, t.services_amount, t.expenses_amount,
           (select s.state from delivery_state s where s.root_table = 'invoice' and s.root_id = i.id order by s.delivery_id desc limit 1) as last_export
    from invoice i
    left join project p on p.id = i.project_id
    left join lateral (
        select count(*) as activity_count, coalesce(sum(a.billed_min), 0) as billed_min, coalesce(sum(a.amount), 0) as services_amount,
               coalesce(sum((select sum(e.billed_amount) from expense_p e where e.activity_id = a.id)), 0) as expenses_amount
        from activity_p a
        where a.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = i.id))
    ) t on true
) q;
comment on view invoice_p is 'Projection of invoice: client, state, totals of its activities and expenses, export state and conditions.';
comment on column invoice_p.account_id is 'Client: the account of the project.';
comment on column invoice_p.state is 'Draft or invoiced.';
comment on column invoice_p.activity_count is 'Number of activities.';
comment on column invoice_p.billed_min is 'Billed duration of its activities.';
comment on column invoice_p.services_amount is 'Amount of its activities.';
comment on column invoice_p.expenses_amount is 'Amount billed of the expenses of its activities.';
comment on column invoice_p.total_amount is 'Services plus expenses.';
comment on column invoice_p.export_state is 'State of the last export to the accounting system.';
comment on column invoice_p.is_draft is 'Draft.';
comment on column invoice_p.is_invoiced is 'Invoiced.';
comment on column invoice_p.has_activities is 'Has at least one activity.';
comment on column invoice_p.not_exported is 'Never sent to the accounting system.';

create view invoice_document as
select i.id as invoice_id,
       jsonb_build_object(
           'epoch', extract(epoch from coalesce(i.invoiced_at, i.date_to::timestamptz))::bigint,
           'invoice', jsonb_build_object('id', i.id, 'state', i.state, 'date_from', i.date_from, 'date_to', i.date_to,
                                         'invoiced_on', i.invoiced_at::date),
           'company', jsonb_build_object('name', c.name, 'address', c.address, 'vat_no', c.vat_no, 'currency', c.currency),
           'account', jsonb_build_object('external_id', a.external_id, 'name', a.name, 'address', a.address),
           'project', jsonb_build_object('external_id', p.external_id, 'name', p.name),
           'lines', (select coalesce(jsonb_agg(jsonb_build_object('date', x.date, 'user', u.name, 'task', t.name, 'folder', f.name,
                                                                  'description', x.description, 'minutes', x.billed_min, 'rate', x.rate, 'amount', x.amount)
                                               order by x.date, x.start_tod, x.id), '[]')
                     from activity_p x
                     join "user" u on u.id = x.user_id
                     join task t on t.id = x.task_id
                     join folder f on f.id = x.folder_id
                     where x.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = i.id))),
           'expenses', (select coalesce(jsonb_agg(jsonb_build_object('date', x.date, 'user', u.name, 'type', et.name, 'quantity', e.quantity,
                                                                     'amount', e.billed_amount)
                                                  order by x.date, e.id), '[]')
                        from expense_p e
                        join activity x on x.id = e.activity_id
                        join expense_type et on et.id = e.expense_type_id
                        join "user" u on u.id = x.user_id
                        where e.activity_id = any(array(select l.activity_id from invoice_item l where l.invoice_id = i.id)) and e.billed_amount <> 0),
           'totals', jsonb_build_object('minutes', i.billed_min, 'services', i.services_amount, 'expenses', i.expenses_amount,
                                        'total', i.total_amount)
       ) as document
from invoice_p i
left join project p on p.id = i.project_id
left join account a on a.id = p.account_id
left join company c on c.id = 1;
comment on view invoice_document is 'The document of an invoice, as JSON: company, client, project, period, activity lines, expense lines and totals. Input of the PDF and of the accounting export.';
comment on column invoice_document.invoice_id is 'Invoice.';
comment on column invoice_document.document is 'The document.';

create view activity_revenue as
select q.*, q.realized_amount - q.invoiced_amount as balance_amount
from (
    select r.*, r.services_amount + r.expense_billed_amount as realized_amount,
           case when r.invoice_id is null then 0 else r.services_amount + r.expense_billed_amount end as invoiced_amount
    from (
        select a.id as activity_id, a.user_id, a.date, a.project_id, a.folder_id, a.invoice_id,
               a.duration_min::integer as duration_min, a.billed_min::integer as billed_min, a.counted_min,
               a.amount as services_amount, 0::numeric as expense_billed_amount, 0::numeric as expense_user_amount
        from activity_p a
        union all
        select a.id, a.user_id, a.date, a.project_id, a.folder_id, a.invoice_id,
               0, 0, 0, 0::numeric, e.billed_amount, e.user_amount
        from expense_p e
        join activity_p a on a.id = e.activity_id
    ) r
) q;
comment on view activity_revenue is 'Revenue lines of the activities: one line per activity (services) and one per expense (expenses billed and reimbursed); realized, invoiced and balance per line.';
comment on column activity_revenue.activity_id is 'Activity of the line.';
comment on column activity_revenue.duration_min is 'Real duration (activity line).';
comment on column activity_revenue.billed_min is 'Billed duration (activity line).';
comment on column activity_revenue.counted_min is 'Counted duration (activity line).';
comment on column activity_revenue.services_amount is 'Amount of the activity (activity line).';
comment on column activity_revenue.expense_billed_amount is 'Expense billed (expense line).';
comment on column activity_revenue.expense_user_amount is 'Expense reimbursed to the user (expense line).';
comment on column activity_revenue.realized_amount is 'Services plus expenses billed.';
comment on column activity_revenue.invoiced_amount is 'Realized amount of a line of an invoiced activity, else 0.';
comment on column activity_revenue.balance_amount is 'Realized minus invoiced.';

create view activity_cost as
select q.*, q.services_amount - q.cost_amount as services_margin_amount, q.balance_amount - q.cost_amount as balance_margin_amount
from (
    select r.*, c.hourly_cost, coalesce(round(r.counted_min * c.hourly_cost / 60.0, 2), 0) as cost_amount
    from activity_revenue r
    left join cost_rate_span c on c.user_id = r.user_id and r.date >= c.valid_from and r.date < c.valid_to
) q;
comment on view activity_cost is 'Revenue lines with the cost of each activity: counted minutes times the hourly cost in force at its date (0 on expense lines). Sensitive.';
comment on column activity_cost.hourly_cost is 'Hourly cost of the user at the date.';
comment on column activity_cost.cost_amount is 'Cost of the activity.';
comment on column activity_cost.services_margin_amount is 'Services minus cost.';
comment on column activity_cost.balance_margin_amount is 'Balance minus cost.';
