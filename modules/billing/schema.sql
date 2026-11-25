create type invoice_state as enum ('draft', 'invoiced');
comment on type invoice_state is 'State of an invoice: draft (its activities are candidates) or invoiced (activities attached, values frozen).';
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

alter table activity add constraint activity_invoice_id_fkey foreign key (invoice_id) references invoice;

create view invoice_line as
select i.id as invoice_id, a.id as activity_id
from invoice i
join activity a on a.invoice_id = i.id
where i.invoiced_at is not null
union all
select i.id, a.id
from invoice i
join folder f on f.project_id = i.project_id
join activity a on a.folder_id = f.id and a.date between i.date_from and i.date_to and a.invoice_id is null
where i.invoiced_at is null;
comment on view invoice_line is 'Activities of an invoice: attached ones once invoiced; while draft, the activities of its project and period not yet invoiced.';
comment on column invoice_line.invoice_id is 'Invoice.';
comment on column invoice_line.activity_id is 'Activity.';

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

create function invoice_totals(invoice_id bigint, out activity_count bigint, out billed_min bigint,
                               out services_amount numeric, out expenses_amount numeric)
language sql stable as $$
  select count(*), coalesce(sum(a.billed_min), 0), coalesce(sum(a.amount), 0),
         coalesce(sum((select sum(e.billed_amount) from expense_p e where e.activity_id = a.id)), 0)
  from invoice_line l
  join activity_p a on a.id = l.activity_id
  where l.invoice_id = invoice_totals.invoice_id $$;
comment on function invoice_totals(bigint) is 'Number, billed duration, amount and expenses billed of the activities of an invoice.';

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
    left join lateral invoice_totals(i.id) t on true
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
                     from invoice_line l
                     join activity_p x on x.id = l.activity_id
                     join "user" u on u.id = x.user_id
                     join task t on t.id = x.task_id
                     join folder f on f.id = x.folder_id
                     where l.invoice_id = i.id),
           'expenses', (select coalesce(jsonb_agg(jsonb_build_object('date', x.date, 'user', u.name, 'type', et.name, 'quantity', e.quantity,
                                                                     'amount', e.billed_amount)
                                                  order by x.date, e.id), '[]')
                        from invoice_line l
                        join activity x on x.id = l.activity_id
                        join expense_p e on e.activity_id = x.id
                        join expense_type et on et.id = e.expense_type_id
                        join "user" u on u.id = x.user_id
                        where l.invoice_id = i.id and e.billed_amount <> 0),
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
    select r.*, r.services_amount + r.expenses_billed as realized_amount,
           case when r.invoice_id is null then 0 else r.services_amount + r.expenses_billed end as invoiced_amount
    from (
        select a.id as activity_id, a.user_id, a.date, a.project_id, a.folder_id, a.invoice_id,
               a.duration_min::integer as duration_min, a.billed_min::integer as billed_min, a.counted_min,
               a.amount as services_amount, 0::numeric as expenses_billed, 0::numeric as expenses_user
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
comment on column activity_revenue.expenses_billed is 'Expense billed (expense line).';
comment on column activity_revenue.expenses_user is 'Expense reimbursed to the user (expense line).';
comment on column activity_revenue.realized_amount is 'Services plus expenses billed.';
comment on column activity_revenue.invoiced_amount is 'Realized amount of a line of an invoiced activity, else 0.';
comment on column activity_revenue.balance_amount is 'Realized minus invoiced.';

create view activity_cost as
select q.*, q.services_amount - q.cost_amount as margin_services, q.balance_amount - q.cost_amount as margin_balance
from (
    select r.*, c.hourly_cost, coalesce(round(r.counted_min * c.hourly_cost / 60.0, 2), 0) as cost_amount
    from activity_revenue r
    left join cost_rate_span c on c.user_id = r.user_id and r.date >= c.valid_from and r.date < c.valid_to
) q;
comment on view activity_cost is 'Revenue lines with the cost of each activity: counted minutes times the hourly cost in force at its date (0 on expense lines). Sensitive.';
comment on column activity_cost.hourly_cost is 'Hourly cost of the user at the date.';
comment on column activity_cost.cost_amount is 'Cost of the activity.';
comment on column activity_cost.margin_services is 'Services minus cost.';
comment on column activity_cost.margin_balance is 'Balance minus cost.';
