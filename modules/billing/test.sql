-- Semantics of the billing module. Run from the repository root with psql on a migrated database with an empty domain:
-- the queries of gen/sqlc.json are prepared under their lowercase names; everything is rolled back.
\set ON_ERROR_STOP on
\o /dev/null
\set manifest `cat gen/sqlc.json`
begin;
select format('prepare %s as %s', lower(q->>'name'), q->>'text') from jsonb_array_elements(:'manifest'::jsonb -> 'queries') q \gexec
set local sor.now = '2026-03-20 12:00';

create function pg_temp.eq(label text, got anycompatible, want anycompatible) returns void language plpgsql as $$
begin
  if got is distinct from want then raise exception '%: got %, want %', label, got, want; end if;
end $$;
create function pg_temp.uid(s text) returns bigint language sql as $$ select id from "user" where subject = s $$;
create function pg_temp.p(d text) returns activity_p language sql as $$ select * from activity_p where description = d $$;
create function pg_temp.inv(d date) returns invoice_p language sql as $$ select * from invoice_p where date_from = d $$;

insert into company overriding system value values (1, 'Test SA');
insert into "user" (subject, name) values ('fin', 'Finance'), ('bob', 'Bob');
insert into task (name, billing, rate) values ('dev', 'always', 100);
insert into account (external_id, name) values ('A', 'Client');
insert into project (external_id, account_id, name) select 'P1', id, 'Project' from account;
insert into folder (project_id, name, rate) select id, 'F1', 100 from project;
insert into folder_task (folder_id, task_id) select f.id, t.id from folder f, task t;
insert into expense_type (name, by_quantity, unit_price, billing_unit_price, mode) values
    ('meal', false, null, null, 'both'), ('km', true, 0.70, 0.90, 'both'), ('parking', false, null, null, 'user_only');
insert into cost_rate (user_id, valid_from, hourly_cost) values (pg_temp.uid('bob'), '2026-01-01', 60);
insert into activity (user_id, date, start_tod, duration_min, folder_id, task_id, description)
select pg_temp.uid('bob'), v.d::date, 600, v.m, f.id, t.id, v.k
from (values ('a1', '2026-02-03', 60), ('a2', '2026-02-10', 120), ('a3', '2026-03-03', 60)) v(k, d, m), folder f, task t;
insert into expense (activity_id, expense_type_id, quantity, amount)
select a.id, t.id, v.q, v.a from (values ('meal', null, 50), ('km', 10, null), ('parking', null, 7)) v(t, q, a)
join expense_type t on t.name = v.t, activity a where a.description = 'a2';

-- Expense amounts by mode.
select pg_temp.eq('expense amounts', (select string_agg(user_amount || '/' || billed_amount, ',' order by id) from expense_p), '50/50,7.00/9.00,7/0');

-- A draft invoice takes the activities of its project and period not yet invoiced.
insert into invoice (project_id, date_from, date_to) select id, '2026-02-01', '2026-02-28' from project;
select pg_temp.eq('draft', (select row(state, activity_count, billed_min, services_amount, expenses_amount, total_amount, export_state)::text
    from pg_temp.inv('2026-02-01')), '(draft,2,180,300.00,59.00,359.00,none)');
select pg_temp.eq('draft conditions', (select row(is_draft, is_invoiced, has_activities, not_exported)::text from pg_temp.inv('2026-02-01')), '(t,f,t,t)');
create temp table ia as execute invoiceactivity((pg_temp.inv('2026-02-01')).id);
select pg_temp.eq('draft lines', (select string_agg(description, ',' order by description) from ia), 'a1,a2');

-- Marking invoiced: lock, attach, freeze activities and expenses, stamp.
create temp table ids as select array[(pg_temp.inv('2026-02-01')).id] as ids;
select format('execute %s(%s)', q, (select quote_literal(ids) from ids)) from unnest(array['invoicelockactivities', 'invoiceattachactivities',
    'invoicefreezeactivities', 'invoicefreezeexpenses']) q \gexec
select format('execute invoicemarkinvoiced(%s, %s)', pg_temp.uid('fin'), (select quote_literal(ids) from ids)) \gexec
select pg_temp.eq('invoiced', (select row(state, activity_count, services_amount, expenses_amount, total_amount)::text
    from pg_temp.inv('2026-02-01')), '(invoiced,2,300.00,59.00,359.00)');
select pg_temp.eq('invoiced conditions', (select row(is_draft, is_invoiced, has_activities, not_exported)::text from pg_temp.inv('2026-02-01')), '(f,t,t,t)');
select pg_temp.eq('activity invoiced', (pg_temp.p('a1')).not_invoiced, false);
select pg_temp.eq('frozen', (select row(frozen_rate, frozen_billed_min, frozen_amount)::text from activity where description = 'a2'), '(100.00,120,200.00)');

-- Frozen values win after a rate change; activities not invoiced follow it.
update folder set rate = 200;
update expense_type set billing_unit_price = 2 where name = 'km';
select pg_temp.eq('frozen amount kept', (pg_temp.p('a1')).amount, 100.00);
select pg_temp.eq('frozen rate kept', (pg_temp.p('a1')).rate, 100.00);
select pg_temp.eq('not invoiced follows', (pg_temp.p('a3')).amount, 200.00);
select pg_temp.eq('frozen expense kept', (pg_temp.inv('2026-02-01')).expenses_amount, 59.00);

-- The document of the invoice.
create temp table doc as execute invoicedocument((pg_temp.inv('2026-02-01')).id);
select pg_temp.eq('document totals', (select document -> 'totals' from doc), '{"total": 359.00, "minutes": 180, "services": 300.00, "expenses": 59.00}'::jsonb);
select pg_temp.eq('document lines', (select jsonb_array_length(document -> 'lines') || '/' || jsonb_array_length(document -> 'expenses') from doc), '2/2');
select pg_temp.eq('document client', (select document #>> '{account,name}' from doc), 'Client');

-- Profitability over February and March.
create temp table pf as execute profitability('2026-02-01', '2026-03-31');
select pg_temp.eq('profitability', (select row(duration_min, services_amount, expenses_billed, expenses_user, realized_amount, invoiced_amount, balance_amount)::text from pf),
    '(240,500.00,59.00,64.00,559.00,359.00,200.00)');
create temp table pc as execute profitabilitycost('2026-02-01', '2026-03-31');
select pg_temp.eq('profitability cost', (select row(cost_amount, margin_services, margin_balance)::text from pc), '(240.00,260.00,-40.00)');

-- Run candidates: none while a draft overlaps; the project once it is gone.
insert into invoice (project_id, date_from, date_to) select id, '2026-03-01', '2026-03-31' from project;
create temp table rc1 as execute invoiceruncandidates('2026-03-01', '2026-03-31');
select pg_temp.eq('candidates with draft', (select count(*) from rc1), 0::bigint);
delete from invoice where date_from = '2026-03-01';
create temp table rc2 as execute invoiceruncandidates('2026-03-01', '2026-03-31');
select pg_temp.eq('candidates', (select count(*) from rc2), 1::bigint);
create temp table rc3 as execute invoiceruncandidates('2026-02-01', '2026-02-28');
select pg_temp.eq('nothing left to invoice', (select count(*) from rc3), 0::bigint);

-- Back to draft releases the activities and their frozen values; amounts follow the new rates.
select format('execute %s(%s)', q, (select quote_literal(ids) from ids)) from unnest(array['invoicelockactivities', 'invoicereleaseexpenses',
    'invoicereleaseactivities', 'invoicebacktodraft']) q \gexec
select pg_temp.eq('back to draft', (select row(state, activity_count, services_amount, expenses_amount)::text from pg_temp.inv('2026-02-01')), '(draft,2,600.00,70.00)');
select pg_temp.eq('released', (select count(*) from activity where invoice_id is not null or frozen_amount is not null), 0::bigint);
select pg_temp.eq('released expenses', (select count(*) from expense where frozen_billed_amount is not null), 0::bigint);

-- An invoice without activity is not invoiceable.
insert into invoice (project_id, date_from, date_to) select id, '2025-01-01', '2025-01-31' from project;
select pg_temp.eq('empty', (pg_temp.inv('2025-01-01')).has_activities, false);

\echo billing: ok
rollback;
