-- Semantics of the time module. Run from the repository root with psql on a migrated database with an empty domain:
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
create function pg_temp.aid(d text) returns bigint language sql as $$ select id from activity where description = d $$;
create function pg_temp.p(d text) returns activity_p language sql as $$ select * from activity_p where description = d $$;

-- Fixtures. 2026-03-02 is a Monday and a full public holiday, 2026-03-04 a partial one, 2026-03-07 a Saturday.
insert into company overriding system value values (1, 'Test SA');
insert into "user" (subject, name) values ('boss', 'Boss'), ('alice', 'Alice'), ('bob', 'Bob'), ('dave', 'Dave');
update "user" set manager_id = pg_temp.uid('boss') where subject <> 'boss';
insert into task (name, kind, billing, requires_validation, counts_hours, rate) values
    ('dev', 'service', 'always', false, true, 100), ('write', 'service', 'sometimes', false, true, null),
    ('free', 'service', 'never', false, true, null), ('check', 'service', 'always', true, true, 100),
    ('vac', 'vacation', 'never', false, true, null), ('oncall', 'service', 'always', false, false, 40);
insert into account (external_id, name) values ('A', 'Client');
insert into project (external_id, account_id, name, rate, entry_closed, archived)
select v.x, a.id, v.x, v.rate, v.closed, v.archived from account a,
    (values ('P1', 80, false, false), ('P2', null, false, false), ('P3', null, true, false), ('P4', null, false, true)) v(x, rate, closed, archived);
insert into folder (project_id, name, rate, estimated_min, entry_closed)
select p.id, v.name, v.rate, v.est, v.closed from (values
    ('F1', 'P1', 90, null, false), ('F2', 'P1', null, null, false), ('F3', 'P1', null, 100, false), ('F4', 'P2', null, null, false),
    ('F5', 'P1', null, null, true), ('F6', 'P3', null, null, false), ('F7', 'P4', null, null, false)) v(name, project, rate, est, closed)
join project p on p.external_id = v.project;
insert into folder_task (folder_id, task_id)
select f.id, t.id from folder f cross join task t where not (f.name = 'F2' and t.name = 'free');
insert into user_task_rate (user_id, task_id, rate) select pg_temp.uid('alice'), id, 150 from task where name = 'dev';
insert into hour_class (label, hours_coef, rate_coef, requires_validation) values ('night', 1.25, 1.5, true), ('saturday', 1, 1.25, false), ('sunday', 2, 2, true);
insert into hour_class_slot (hour_class_id, day_type, span_tod)
select c.id, d, r::int4range from hour_class c, generate_series(1, 8) d, unnest(array['[0,360)', '[1200,1440)']) r where c.label = 'night'
union all select c.id, 6, '[360,1200)' from hour_class c where c.label = 'saturday'
union all select c.id, d, '[360,1200)' from hour_class c, generate_series(7, 8) d where c.label = 'sunday';
insert into public_holiday (date, label, deducted_min) values ('2026-03-02', 'full', null), ('2026-03-04', 'partial', 240);
insert into period_lock (date_from, date_to, user_id) values ('2026-01-01', '2026-01-31', null), ('2026-02-10', '2026-02-12', pg_temp.uid('bob'));
insert into work_period (user_id, valid_from, week_min, vacation_days, activity_pct) values
    (pg_temp.uid('dave'), '2026-01-05', '{480,480,480,480,480,0,0}', 20, 100),
    (pg_temp.uid('dave'), '2026-03-02', '{480,480,480,480,480,0,0}', 20, 50);

insert into activity (user_id, date, start_tod, duration_min, billed_min, folder_id, task_id, description)
select pg_temp.uid(v.u), v.d::date, v.s, v.m, v.b, f.id, t.id, v.k
from (values
    ('r1', 'alice', '2026-03-03', 600, 60, null, 'F1', 'dev'), ('r2', 'bob', '2026-03-03', 600, 60, null, 'F1', 'dev'),
    ('r3', 'bob', '2026-03-03', 600, 60, null, 'F2', 'dev'), ('r4', 'bob', '2026-03-03', 600, 60, null, 'F4', 'dev'),
    ('b1', 'bob', '2026-03-03', 660, 60, 30, 'F1', 'write'), ('b2', 'bob', '2026-03-03', 720, 60, null, 'F1', 'write'),
    ('b3', 'bob', '2026-03-03', 780, 60, null, 'F1', 'free'), ('s1', 'bob', '2026-03-03', 1140, 120, null, 'F1', 'dev'),
    ('s2', 'bob', '2026-03-07', 600, 60, null, 'F1', 'dev'), ('s3', 'bob', '2026-03-02', 600, 60, null, 'F1', 'dev'),
    ('s4', 'bob', '2026-03-04', 600, 60, null, 'F1', 'dev'), ('oc', 'bob', '2026-03-03', 900, 60, null, 'F1', 'oncall'),
    ('v1', 'bob', '2026-03-03', 960, 60, null, 'F1', 'check'),
    ('e1', 'bob', '2026-03-05', 600, 60, null, 'F3', 'dev'), ('e2', 'bob', '2026-03-05', 700, 60, null, 'F3', 'dev'),
    ('l1', 'bob', '2026-01-31', 600, 60, null, 'F1', 'dev'), ('l2', 'bob', '2026-02-01', 600, 60, null, 'F1', 'dev'),
    ('l3', 'bob', '2026-02-10', 600, 60, null, 'F1', 'dev'), ('l4', 'bob', '2026-02-12', 600, 60, null, 'F1', 'dev'),
    ('l5', 'bob', '2026-02-13', 600, 60, null, 'F1', 'dev'), ('l6', 'alice', '2026-02-11', 600, 60, null, 'F1', 'dev'),
    ('c1', 'bob', '2026-03-06', 600, 60, null, 'F5', 'dev'), ('c2', 'bob', '2026-03-06', 600, 60, null, 'F6', 'dev'),
    ('c3', 'bob', '2026-03-06', 600, 60, null, 'F7', 'dev'), ('t1', 'bob', '2026-03-06', 600, 60, null, 'F2', 'free'),
    ('d1', 'dave', '2026-02-02', 480, 480, null, 'F1', 'dev'), ('d2', 'dave', '2026-02-03', 480, 480, null, 'F1', 'vac'),
    ('d3', 'dave', '2026-03-03', 480, 300, null, 'F1', 'dev'), ('d4', 'dave', '2026-03-25', 480, 100, null, 'F1', 'dev')
) v(k, u, d, s, m, b, f, t)
join folder f on f.name = v.f join task t on t.name = v.t;

-- Rate: user and task, else folder, else project, else task.
select pg_temp.eq('rate user x task', (pg_temp.p('r1')).rate, 150.00);
select pg_temp.eq('rate folder', (pg_temp.p('r2')).rate, 90.00);
select pg_temp.eq('rate project', (pg_temp.p('r3')).rate, 80.00);
select pg_temp.eq('rate task', (pg_temp.p('r4')).rate, 100.00);
select pg_temp.eq('amount', (pg_temp.p('r1')).amount, 150.00);

-- Billed minutes: always = real, sometimes = entered or real, never = 0.
select pg_temp.eq('billed always', (pg_temp.p('r2')).billed_min, 60);
select pg_temp.eq('billed sometimes entered', (pg_temp.p('b1')).billed_min, 30);
select pg_temp.eq('amount sometimes', (pg_temp.p('b1')).amount, 45.00);
select pg_temp.eq('billed sometimes empty', (pg_temp.p('b2')).billed_min, 60);
select pg_temp.eq('billed never', (pg_temp.p('b3')).billed_min, 0);
select pg_temp.eq('amount never', (pg_temp.p('b3')).amount, 0.00);

-- Straddling a class: 19:00-21:00 on a Tuesday, one hour at night (rate x1.5, hours x1.25).
select pg_temp.eq('straddle amount', (pg_temp.p('s1')).amount, 225.00);
select pg_temp.eq('straddle counted', (pg_temp.p('s1')).counted_min, 135);
select pg_temp.eq('weekday counted', (pg_temp.p('r2')).counted_min, 60);
select pg_temp.eq('saturday day type', (pg_temp.p('s2')).day_type, 6);
select pg_temp.eq('saturday counted', (pg_temp.p('s2')).counted_min, 60);
select pg_temp.eq('saturday amount', (pg_temp.p('s2')).amount, 112.50);
select pg_temp.eq('holiday day type', (pg_temp.p('s3')).day_type, 8);
select pg_temp.eq('holiday counted', (pg_temp.p('s3')).counted_min, 120);
select pg_temp.eq('holiday amount', (pg_temp.p('s3')).amount, 180.00);
select pg_temp.eq('partial holiday day type', (pg_temp.p('s4')).day_type, 3);
select pg_temp.eq('task not counting hours', (pg_temp.p('oc')).counted_min, 0);
select pg_temp.eq('task not counting hours amount', (pg_temp.p('oc')).amount, 90.00);

-- The split by class sums back to the activity.
select pg_temp.eq('split minutes', (select sum(duration_min) from activity_class where activity_id = pg_temp.aid('s1')), 120::bigint);
select pg_temp.eq('split counted', (select round(sum(counted_min)) from activity_class where activity_id = pg_temp.aid('s1')), 135::numeric);
select pg_temp.eq('split amount', (select sum(amount) from activity_class where activity_id = pg_temp.aid('s1')), 225.00);
select pg_temp.eq('split rows', (select count(*) from activity_class where activity_id = pg_temp.aid('s1')), 2::bigint);
select pg_temp.eq('split inside one class', (select count(*) || '/' || sum(duration_min) from activity_class where activity_id = pg_temp.aid('s3') and hour_class_id is not null), '1/60');
select pg_temp.eq('split outside', (select count(*) from activity_class where activity_id = pg_temp.aid('s3') and hour_class_id is null), 0::bigint);
create temp table ac as execute activityclass(pg_temp.aid('s1'), array[pg_temp.uid('bob')]);
select pg_temp.eq('activity_class view', (select string_agg(coalesce(label, '-') || ':' || duration_min, ',' order by label nulls first) from ac), '-:60,night:60');

-- To validate: by the task, by a class requiring it, by the folder estimate exceeded; else not.
select pg_temp.eq('to_validate task', (pg_temp.p('v1')).to_validate, true);
select pg_temp.eq('to_validate class', (pg_temp.p('s1')).to_validate, true);
select pg_temp.eq('to_validate class not requiring', (pg_temp.p('s2')).to_validate, false);
select pg_temp.eq('to_validate estimate', (pg_temp.p('e1')).to_validate and (pg_temp.p('e2')).to_validate, true);
select pg_temp.eq('to_validate none', (pg_temp.p('r2')).to_validate, false);
update folder set estimated_min = 120 where name = 'F3';
select pg_temp.eq('to_validate estimate reached', (pg_temp.p('e1')).to_validate, false);

-- Conditions.
select pg_temp.eq('locked global end', (pg_temp.p('l1')).not_locked, false);
select pg_temp.eq('after global lock', (pg_temp.p('l2')).not_locked, true);
select pg_temp.eq('locked user start', (pg_temp.p('l3')).not_locked, false);
select pg_temp.eq('locked user end', (pg_temp.p('l4')).not_locked, false);
select pg_temp.eq('after user lock', (pg_temp.p('l5')).not_locked, true);
select pg_temp.eq('other user not locked', (pg_temp.p('l6')).not_locked, true);
select pg_temp.eq('entry open', (pg_temp.p('r2')).entry_open, true);
select pg_temp.eq('folder closed', (pg_temp.p('c1')).entry_open, false);
select pg_temp.eq('project closed', (pg_temp.p('c2')).entry_open, false);
select pg_temp.eq('project archived', (pg_temp.p('c3')).entry_open, false);
select pg_temp.eq('task allowed', (pg_temp.p('r3')).task_allowed, true);
select pg_temp.eq('task not allowed', (pg_temp.p('t1')).task_allowed, false);
select pg_temp.eq('not invoiced', (pg_temp.p('r2')).not_invoiced, true);
select pg_temp.eq('not validated', (pg_temp.p('r2')).not_validated and not (pg_temp.p('r2')).is_validated, true);
execute activityvalidate(pg_temp.uid('boss'), array[pg_temp.aid('r2'), pg_temp.aid('v1')]);
select pg_temp.eq('validated', (pg_temp.p('r2')).is_validated and not (pg_temp.p('r2')).not_validated, true);
create temp table tv as execute activitytovalidate('2026-03-01', '2026-03-31', array[pg_temp.uid('bob')]);
select pg_temp.eq('to validate list', (select string_agg(description, ',' order by description) from tv), 's1,s3');
execute activityunvalidate(array[pg_temp.aid('r2')]);
select pg_temp.eq('unvalidated', (pg_temp.p('r2')).not_validated, true);

-- Days of a user: due after holidays and activity rate, counted, locked.
create temp table td as execute timesheetday(pg_temp.uid('dave'), '2026-03-02', '2026-03-04', array[pg_temp.uid('dave')]);
select pg_temp.eq('day due', (select string_agg(due_min || '/' || done_min || '/' || coalesce(holiday, '-'), ',' order by date) from td), '0/0/full,240/300/-,120/0/partial');
create temp table tl as execute timesheetday(pg_temp.uid('bob'), '2026-02-09', '2026-02-10', array[pg_temp.uid('bob')]);
select pg_temp.eq('day locked', (select string_agg(locked::text, ',' order by date) from tl), 'false,true');
create temp table tn as execute timesheetday(pg_temp.uid('bob'), '2026-02-09', '2026-02-10', array[pg_temp.uid('alice')]);
select pg_temp.eq('day out of reach', (select count(*) from tn), 0::bigint);

-- Balances of dave over March, today 2026-03-20: 100 % from 2026-01-05, 50 % from 2026-03-02.
-- Carried: 40 weekdays x 480 due, 960 done -> -18240. Due: 15 weekdays x 480 - 480 (full) - 240 (partial) = 6480 at 50 % -> 3240.
-- Vacation: 55/365 x 20 x 480 - 480 taken -> 967 carried; (1/365 x 9600 + 19/365 x 4800) -> 276 available.
create temp table bal as execute balance('2026-03-01', '2026-03-31', array[pg_temp.uid('dave')]);
select pg_temp.eq('balance', (select row(carried_min, due_min, done_min, balance_min)::text from bal), '(-18240,3240,300,-21180)');
select pg_temp.eq('vacation balance', (select row(vac_carried_min, vac_available_min, vac_taken_min, vac_balance_min)::text from bal), '(967,276,0,1243)');
create temp table bal2 as execute balance('2026-01-05', '2026-03-20', array[pg_temp.uid('dave')]);
select pg_temp.eq('balance whole history', (select row(carried_min, due_min, done_min, vac_taken_min)::text from bal2), '(0,22440,1260,480)');

\echo time: ok
rollback;
