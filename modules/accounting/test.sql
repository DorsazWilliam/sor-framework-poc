-- Semantics of the accounting module. Run from the repository root with psql on a migrated database with an empty domain:
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

-- Pull: upserts by external id create, then rename.
execute accountupsertfromaccounting('A0001', 'Provisional', '');
execute accountupsertfromaccounting('A0001', 'Real name', 'Street 1');
execute projectupsertfromaccounting('P00001', 'Real project', 'A0001');
execute projectupsertfromaccounting('P00002', 'Unknown client', 'A9999');
select pg_temp.eq('accounts', (select string_agg(external_id || ':' || name || ':' || address, ',') from account), 'A0001:Real name:Street 1');
select pg_temp.eq('projects', (select string_agg(p.external_id || ':' || p.name || ':' || a.external_id, ',') from project p join account a on a.id = p.account_id), 'P00001:Real project:A0001');

insert into connection (kind, name, endpoint, subject) values ('accounting', 'Accounting', 'http://acct', 'svc');
create temp table cid as select id from connection;
select format('execute connectionadvance(%L, null, %L)', 'c1', array[(select id from cid)]) \gexec
select format('execute connectionadvance(null, %L, %L)', 'down', array[(select id from cid)]) \gexec
select pg_temp.eq('advance keeps the cursor', (select cursor || '/' || pull_error from connection), 'c1/down');

-- Push: an invoiced invoice is exported as its document; the state follows the results.
insert into company overriding system value values (1, 'Test SA');
insert into "user" (subject, name) values ('fin', 'Finance');
insert into task (name, rate) values ('dev', 100);
insert into folder (project_id, name) select id, 'F' from project;
insert into activity (user_id, date, start_tod, duration_min, folder_id, task_id)
select u.id, '2026-02-03', 600, 60, f.id, t.id from "user" u, folder f, task t;
insert into invoice (project_id, date_from, date_to) select id, '2026-02-01', '2026-02-28' from project;
create temp table ids as select array_agg(id) as ids from invoice;
select format('execute %s(%L)', q, (select ids from ids)) from unnest(array['invoicelockactivities', 'invoiceattachactivities', 'invoicefreezeactivities']) q \gexec
select format('execute invoicemarkinvoiced(%s, %L)', (select id from "user"), (select ids from ids)) \gexec
select pg_temp.eq('not exported', (select export_state::text || '/' || not_exported from invoice_p), 'none/true');
select format('execute invoiceexport(%s, %L)', (select id from "user"), (select ids from ids)) \gexec
select pg_temp.eq('delivery', (select kind || '/' || root_table || '/' || (payload #>> '{totals,total}')::numeric from delivery), 'invoice/invoice/100.00');
select pg_temp.eq('created', (select export_state::text || '/' || not_exported from invoice_p), 'created/false');
insert into delivery_result (delivery_id, ok, status, detail) select id, false, 503, 'down' from delivery;
select pg_temp.eq('failed', (select export_state from invoice_p), 'failed'::export_state);
insert into delivery_result (delivery_id, ok, status, detail) select id, true, 201, 'ok' from delivery;
select pg_temp.eq('exported', (select export_state from invoice_p), 'exported'::export_state);
select format('create temp table ae as execute accountingexport(%s)', (select ids[1] from ids)) \gexec
select pg_temp.eq('export view', (select state || '/' || attempts || '/' || last_status from ae), 'exported/2/201');

\echo accounting: ok
rollback;
