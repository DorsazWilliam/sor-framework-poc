-- Synthetic deterministic dataset, relative to sor_today().
-- Settings: seed.size (demo | perf), seed.users, seed.activities, optional seed.acct_url.
-- All names and data are invented.

truncate company, "user", user_group, user_group_member, user_group_fn, account, project, folder, folder_task, task,
         user_task_rate, work_period, cost_rate, public_holiday, hour_class, hour_class_slot,
         activity, expense_type, expense, period_lock, invoice,
         connection, delivery, delivery_result, run, saved_view, event, file
    restart identity cascade;

select setseed(0.42);

create temp table p on commit drop as
select current_setting('seed.users')::integer as users,
       current_setting('seed.activities')::integer as activities,
       sor_today() as today,
       (sor_today() - interval '3 years')::date as start,
       coalesce(nullif(current_setting('seed.acct_url', true), ''), 'http://acct:8070') as acct_url;

insert into company (name, address, vat_no, currency)
values ('Exemple Services SA', E'Rue de l''Exemple 12\n1000 Ville', 'CHE-000.000.000 TVA', 'CHF');

-- Humans: admin, manager, finance, user, then u001...; a three-level manager tree under admin.
insert into "user" (kind, subject, name, manager_id, lang)
select 'human',
       case g when 1 then 'admin' when 2 then 'manager' when 3 then 'finance' when 4 then 'user' else 'u' || lpad((g - 4)::text, 3, '0') end,
       (array['Anne','Marc','Julie','Luc','Sophie','Pierre','Claire','Nicolas','Laura','Thomas',
              'Elena','David','Sarah','Martin','Lea','Simon','Nina','Yves','Carla','Hugo'])[1 + (g * 7) % 20]
       || ' ' ||
       (array['Favre','Muller','Rossi','Meyer','Bonvin','Keller','Fontana','Weber','Rochat','Schmid',
              'Gerber','Moret','Huber','Lambert','Vogel','Perret','Bianchi','Frey','Girard','Steiner'])[1 + (g * 13 + g / 20) % 20],
       null,
       (array['fr','fr','fr','de','en'])[1 + g % 5]::lang
from p, generate_series(1, p.users) g;

-- Manager and finance under admin; user and k2 team leads under manager; staff under the team leads.
update "user" u set manager_id = case
        when u.id in (2, 3) then 1
        when u.id <= 4 + t.k2 then 2
        else 5 + (u.id % t.k2) end
from (select greatest(2, p.users / 10) as k2 from p) t
where u.id > 1;

insert into "user" (kind, subject, name, lang) values
    ('service', 'svc-accounting', 'Service comptabilité', 'fr'),
    ('service', 'svc-ocr', 'Service OCR', 'fr'),
    ('agent', 'agent-user', 'Agent de saisie', 'fr'),
    ('agent', 'agent-manager', 'Agent de validation', 'fr'),
    ('agent', 'agent-auto', 'Agent autonome', 'fr');

insert into user_group (name, admin) values
    ('admin', true), ('manager', false), ('finance', false), ('collaborateur', false),
    ('agents', false), ('service comptabilité', false), ('service ocr', false);
insert into user_group_fn (user_group_id, fn)
select g.id, f::fn
from user_group g
join (values ('manager', array['validation', 'scope_tree']),
             ('finance', array['invoice', 'invoice_state', 'cost', 'scope_all']),
             ('collaborateur', array['activity', 'expense']),
             ('agents', array['scope_all', 'activity', 'expense']),
             ('service comptabilité', array['accounting_sync']),
             ('service ocr', array['ocr', 'scope_all'])) v(name, fns) on v.name = g.name
cross join unnest(v.fns) f;
insert into user_group_member (user_group_id, user_id)
select g.id, u.id
from "user" u
join user_group g on g.name = case
        when u.subject = 'admin' then 'admin'
        when u.subject = 'finance' then 'finance'
        when u.kind = 'agent' then 'agents'
        when u.subject = 'svc-accounting' then 'service comptabilité'
        when u.subject = 'svc-ocr' then 'service ocr'
        when exists (select from "user" s where s.manager_id = u.id) then 'manager' end
union
select g.id, u.id from "user" u join user_group g on g.name = 'collaborateur' where u.kind = 'human' and u.subject <> 'admin';

-- Clients and projects with provisional names; the first accounting pull renames them.
insert into account (external_id, name, address)
select 'A' || lpad(g::text, 4, '0'), 'Compte A' || lpad(g::text, 4, '0'), 'Ville ' || (1 + g % 40)
from generate_series(1, 300) g;
insert into project (external_id, account_id, name, rate, entry_closed, archived, lead_user_id)
select 'P' || lpad(g::text, 5, '0'), 1 + (g - 1) / 5, 'Projet P' || lpad(g::text, 5, '0'),
       case when random() < 0.3 then 100 + 10 * floor(random() * 12) end,
       g % 50 = 7, g % 50 = 9, 1 + g % p.users
from p, generate_series(1, 1500) g;
insert into folder (project_id, parent_id, name, rate, entry_closed)
select 1 + (g - 1) / 4, case when g % 4 = 0 then g - 1 end,
       (array['Analyse','Développement','Support','Gestion de projet','Formation','Tests','Exploitation','Migration'])[1 + g % 8],
       case when random() < 0.2 then 110 + 10 * floor(random() * 10) end,
       g % 100 = 13
from generate_series(1, 6000) g;

-- Internal client for absences, vacation and internal work; never billed.
insert into account (external_id, name) values ('INT', 'Interne');
insert into project (external_id, account_id, name) values ('INT', 301, 'Interne');
insert into folder (project_id, name) values (1501, 'Vacances'), (1501, 'Absences'), (1501, 'Administration');

insert into task (name, kind, billing, requires_validation, counts_hours, rate) values
    ('Analyse', 'service', 'always', false, true, 150), ('Conception', 'service', 'always', false, true, 160),
    ('Développement', 'service', 'always', false, true, 140), ('Tests', 'service', 'always', false, true, 130),
    ('Documentation', 'service', 'always', false, true, 120), ('Réunion client', 'service', 'always', false, true, 150),
    ('Gestion de projet', 'service', 'always', false, true, 170), ('Support', 'service', 'always', false, true, 120),
    ('Maintenance', 'service', 'always', false, true, 130), ('Formation donnée', 'service', 'always', false, true, 180),
    ('Conseil', 'service', 'always', true, true, 200), ('Audit', 'service', 'always', true, true, 190),
    ('Déploiement', 'service', 'always', false, true, 140), ('Intégration', 'service', 'always', false, true, 140),
    ('Paramétrage', 'service', 'always', false, true, 130), ('Revue de code', 'service', 'always', false, true, 140),
    ('Rédaction', 'service', 'sometimes', false, true, 120), ('Coordination', 'service', 'sometimes', false, true, 150),
    ('Déplacement', 'service', 'sometimes', true, true, 100), ('Recherche', 'service', 'sometimes', false, true, 130),
    ('Correction garantie', 'service', 'never', false, true, null), ('Avant-vente', 'service', 'never', false, true, null),
    ('Astreinte', 'service', 'always', false, false, 40), ('Veille technique', 'service', 'never', false, true, null),
    ('Administration interne', 'service', 'never', false, true, null), ('Formation suivie', 'service', 'never', false, true, null),
    ('Vacances', 'vacation', 'never', false, true, null), ('Maladie', 'absence', 'never', false, true, null),
    ('Absence diverse', 'absence', 'never', false, true, null), ('Congé parental', 'absence', 'never', false, true, null);

-- About 12 service tasks allowed per project folder; internal folders allow the internal tasks.
insert into folder_task (folder_id, task_id)
select f.id, t.id
from folder f
cross join lateral (select t.id from task t where t.id <= 24 order by md5(f.id || '-' || t.id) limit 12) t
where f.project_id <= 1500;
insert into folder_task (folder_id, task_id) values (6001, 27), (6002, 28), (6002, 29), (6002, 30), (6003, 25), (6003, 26), (6003, 24);

insert into user_task_rate (user_id, task_id, rate)
select distinct on (x.u, x.t) x.u, x.t, 120 + 10 * floor(random() * 10)
from (select 1 + floor(random() * p.users)::bigint as u, 1 + floor(random() * 20)::bigint as t from p, generate_series(1, p.users * 2) g) x;

insert into expense_type (name, by_quantity, unit_price, billing_unit_price, mode) values
    ('Kilomètres', true, 0.70, 0.90, 'both'), ('Repas', false, null, null, 'both'), ('Hôtel', false, null, null, 'both'),
    ('Train', false, null, null, 'both'), ('Parking', false, null, null, 'user_only'), ('Forfait matériel', true, 0, 25, 'billing_only');

insert into hour_class (label, hours_coef, rate_coef, requires_validation) values
    ('Nuit', 1.25, 1.25, true), ('Samedi', 1.00, 1.25, false), ('Dimanche et férié', 1.50, 1.50, true);
insert into hour_class_slot (hour_class_id, day_type, span_tod)
select 1, d, r::int4range from generate_series(1, 8) d, unnest(array['[0,360)', '[1200,1440)']) r
union all select 2, 6, '[360,1200)'
union all select 3, d, '[360,1200)' from generate_series(7, 8) d;

-- Public holidays of the covered years: fixed dates, Easter-based dates, two partial eves.
insert into public_holiday (date, label, deducted_min)
select h.date, h.label, h.deducted_min
from p
cross join generate_series(extract(year from p.start)::integer, extract(year from p.today)::integer + 1) y
cross join lateral (
    select (y % 19) as a, y / 100 as b, y % 100 as c
) e1
cross join lateral (
    select (19 * e1.a + e1.b - e1.b / 4 - (e1.b - (e1.b + 8) / 25 + 1) / 3 + 15) % 30 as h, e1.b % 4 as e, e1.c / 4 as i, e1.c % 4 as k
) e2
cross join lateral (select (32 + 2 * e2.e + 2 * e2.i - e2.h - e2.k) % 7 as l) e3
cross join lateral (
    select make_date(y, (e2.h + e3.l - 7 * ((e1.a + 11 * e2.h + 22 * e3.l) / 451) + 114) / 31,
                     (e2.h + e3.l - 7 * ((e1.a + 11 * e2.h + 22 * e3.l) / 451) + 114) % 31 + 1) as easter
) e4
cross join lateral (values
    (make_date(y, 1, 1), 'Nouvel an', null::integer), (make_date(y, 1, 2), 'Lendemain de Nouvel an', null),
    (e4.easter - 2, 'Vendredi saint', null), (e4.easter + 1, 'Lundi de Pâques', null),
    (e4.easter + 39, 'Ascension', null), (e4.easter + 50, 'Lundi de Pentecôte', null),
    (make_date(y, 8, 1), 'Fête nationale', null), (make_date(y, 12, 24), 'Veille de Noël', 240),
    (make_date(y, 12, 25), 'Noël', null), (make_date(y, 12, 26), 'Saint-Étienne', null),
    (make_date(y, 12, 31), 'Saint-Sylvestre', 240)
) h(date, label, deducted_min);

-- Work periods: full time by default, some part time, some changing over time.
insert into work_period (user_id, valid_from, week_min, vacation_days, activity_pct)
select u.id, p.start, '{504,504,504,504,504,0,0}', case when u.id % 9 = 0 then 30 else 25 end,
       case when u.id % 7 = 0 then 80 when u.id % 11 = 0 then 60 else 100 end
from "user" u, p
where u.kind = 'human';
insert into work_period (user_id, valid_from, week_min, vacation_days, activity_pct)
select u.id, p.start + (200 + u.id * 37 % 600)::integer, '{504,504,504,504,504,0,0}', 25, case when u.id % 7 = 0 then 100 else 80 end
from "user" u, p
where u.kind = 'human' and u.id % 6 = 1;

insert into cost_rate (user_id, valid_from, hourly_cost)
select u.id, p.start + v.offs, 55 + (u.id * 7 % 40) + v.n * 3
from "user" u, p
cross join lateral (select * from (values (0, 0), (365, 1), (730, 2)) v(offs, n) where v.n <= u.id % 3) v
where u.kind = 'human';

-- Days of each user: working days are filled with activities, some are vacation or absence days.
create temp table ud on commit drop as
select u.id as user_id, d::date as date, w.activity_pct, random() as r, (d::date - p.start) / 30 as month_no
from p
cross join "user" u
cross join generate_series(p.start, p.today, interval '1 day') d
join work_period_span w on w.user_id = u.id and d::date >= w.valid_from and d::date < w.valid_to
where u.kind = 'human' and extract(isodow from d) < 6
  and not exists (select from public_holiday h where h.date = d::date and h.deducted_min is null)
  and random() < w.activity_pct / 100.0;

create temp table fta on commit drop as
select ft.folder_id, array_agg(ft.task_id order by ft.task_id) as tasks
from folder_task ft join folder f on f.id = ft.folder_id
where f.project_id <= 1500
group by ft.folder_id;

-- Activities of working days: k per day around the average that reaches seed.activities, contiguous from about 08:00.
create temp table ag on commit drop as
with k as (
    select (p.activities * 0.97 - count(*) filter (where ud.r < 0.10)) / count(*) filter (where ud.r >= 0.10) as avg_k
    from ud, p group by p.activities
), d as (
    select ud.*, greatest(1, floor(k.avg_k) + case when random() < k.avg_k - floor(k.avg_k) then 1 else 0 end)::integer as n
    from ud, k where ud.r >= 0.10
)
select d.user_id, d.date, d.month_no, s, d.n,
       greatest(15, round(510.0 / d.n * (0.6 + 0.8 * random()) / 15) * 15)::integer as dur
from d cross join generate_series(1, d.n) s;

-- All generated activities, before they are written: working days, vacation and absence days, about 3 % off hours.
create temp table act on commit drop as
select x.user_id, x.date, x.start_tod, least(x.dur, 1440 - x.start_tod) as duration_min,
       case when t.billing = 'sometimes' and random() < 0.5 then greatest(15, round(x.dur * (0.5 + 0.5 * random()) / 15) * 15)::integer end as billed_min,
       x.folder_id, x.task_id,
       (array['Travail selon planning','Séance avec le client','Corrections','Préparation','Suivi des tickets',
              'Analyse des besoins','Mise à jour','Contrôle qualité','Rapport','Échanges par courriel'])[1 + floor(random() * 10)::integer] as description
from (
    select a.user_id, a.date, a.dur, f.folder_id, f.tasks[1 + floor(random() * cardinality(f.tasks))::integer] as task_id,
           least(1425, 465 + floor(random() * 4)::integer * 15
               + coalesce(sum(a.dur) over (partition by a.user_id, a.date order by a.s rows between unbounded preceding and 1 preceding), 0)
               + case when coalesce(sum(a.dur) over (partition by a.user_id, a.date order by a.s rows between unbounded preceding and 1 preceding), 0) >= 240
                      then 45 else 0 end)::integer as start_tod
    from ag a
    cross join lateral (select 1 + (hashint8(a.user_id * 100000 + a.month_no * 10 + floor(random() * 6)::bigint)::bigint & 2147483647) % 6000 as fid) h
    join fta f on f.folder_id = h.fid
) x
join task t on t.id = x.task_id
union all
select ud.user_id, ud.date, 480, round(504 * ud.activity_pct / 100)::integer, null,
       case when ud.r < 0.08 then 6001 else 6002 end, case when ud.r < 0.08 then 27 when ud.r < 0.095 then 28 else 29 end, ''
from ud
where ud.r < 0.10
union all
select x.user_id, x.date, x.start_tod, least(x.dur, 1440 - x.start_tod), null, f.folder_id,
       f.tasks[1 + floor(random() * cardinality(f.tasks))::integer], 'Intervention hors horaire'
from (
    select ud.user_id, ud.month_no,
           case when ud.n % 2 = 0 then ud.date + (6 + ((ud.n / 2) % 2)::integer - extract(isodow from ud.date)::integer) else ud.date end as date,
           case when ud.n % 2 = 0 then 540 + floor(random() * 16)::integer * 30 else 1080 + floor(random() * 8)::integer * 30 end as start_tod,
           60 + floor(random() * 6)::integer * 30 as dur
    from (
        select ud.*, row_number() over (order by ud.date, ud.user_id) / s.step as n, row_number() over (order by ud.date, ud.user_id) % s.step as m
        from ud, (select greatest(1, round(count(*) / (p.activities * 0.03)))::bigint as step from ud, p group by p.activities) s
    ) ud
    where ud.m = 0
) x
cross join lateral (select 1 + (hashint8(x.user_id * 100000 + x.month_no * 10)::bigint & 2147483647) % 6000 as fid) h
join fta f on f.folder_id = h.fid
cross join p
where x.date <= p.today;

-- Invoices of the months older than about three months, invoiced; drafts of last month for a few projects. An invoice takes
-- the billable activities: those whose task is billed.
insert into invoice (project_id, date_from, date_to, invoiced_at, invoiced_by)
select m.project_id, m.month, (m.month + interval '1 month - 1 day')::date, m.month + interval '1 month 5 days', 3
from (
    select distinct f.project_id, date_trunc('month', a.date)::date as month
    from act a join folder f on f.id = a.folder_id join task t on t.id = a.task_id and t.billing <> 'never', p
    where a.date < date_trunc('month', p.today - interval '2 months')
) m
order by m.month, m.project_id;
insert into invoice (project_id, date_from, date_to)
select distinct f.project_id, date_trunc('month', p.today - interval '1 month')::date, (date_trunc('month', p.today) - interval '1 day')::date
from p, act a join folder f on f.id = a.folder_id join task t on t.id = a.task_id and t.billing <> 'never'
where a.date >= date_trunc('month', p.today - interval '1 month') and a.date < date_trunc('month', p.today) and f.project_id % 100 = 1
order by 1;

-- Estimates near the real total on 20 % of the folders: about half are exceeded.
update folder f set estimated_min = round(s.total * (0.8 + 0.45 * random()))::integer
from (select a.folder_id, sum(a.duration_min) as total from act a group by a.folder_id) s
where s.folder_id = f.id and f.id % 5 = 0;

-- Period locks: every past year for all, one month for one user.
insert into period_lock (date_from, date_to, user_id)
select make_date(y, 1, 1), make_date(y, 12, 31), null
from p, generate_series(extract(year from p.start)::integer, extract(year from p.today)::integer - 1) y;
insert into period_lock (date_from, date_to, user_id)
select date_trunc('month', p.today - interval '2 months')::date, (date_trunc('month', p.today - interval '1 month') - interval '1 day')::date, 5
from p;

-- The generated rows reference existing rows only: the bulk load skips the per-row foreign key triggers.
set local session_replication_role = replica;

insert into activity (user_id, date, start_tod, duration_min, billed_min, folder_id, task_id, description)
select a.user_id, a.date, a.start_tod, a.duration_min, a.billed_min, a.folder_id, a.task_id, a.description
from act a
order by a.date, a.user_id, a.start_tod;
analyze activity, folder;

-- Most old activities to validate are validated by their manager; the billed ones of an invoiced invoice are stamped with it
-- and the values of their projection. Both read activity_p, so the rows are rewritten once from it.
create temp table fz on commit drop as
select a.*, x.to_validate, i.id as invoice, x.rate, x.billed_min as billed, x.amount, u.manager_id
from activity a
join activity_p x on x.id = a.id
join "user" u on u.id = a.user_id
join task t on t.id = a.task_id
left join invoice i on t.billing <> 'never' and i.project_id = x.project_id and a.date between i.date_from and i.date_to and i.invoiced_at is not null;
truncate activity, expense;
insert into activity overriding system value
select z.id, z.user_id, z.date, z.start_tod, z.duration_min, z.billed_min, z.folder_id, z.task_id, z.description,
       case when v.ok then coalesce(z.manager_id, 1) end, case when v.ok then (z.date + 3)::timestamptz end,
       z.invoice,
       case when z.invoice is not null then z.rate end, case when z.invoice is not null then z.billed end,
       case when z.invoice is not null then z.amount end
from fz z
cross join p
cross join lateral (select z.to_validate and z.date < p.today - 30 and z.id % 10 < 9 as ok) v
order by z.id;

-- Expenses on about 5 % of the activities of project folders; stamped on invoiced activities.
insert into expense (activity_id, expense_type_id, quantity, amount)
select x.id, x.t,
       case x.t when 1 then 10 + floor(random() * 190) when 6 then 1 + floor(random() * 3) end,
       case when x.t not in (1, 6) then round((8 + random() * 240)::numeric, 2) end
from (select a.id, 1 + floor(random() * 6)::integer as t from activity a where a.folder_id <= 6000 and random() < 0.05) x
order by x.id;
update expense e set (frozen_user_amount, frozen_billed_amount) = (x.user_amount, x.billed_amount)
from expense_p x, activity a
where x.id = e.id and a.id = e.activity_id and a.invoice_id is not null;

insert into connection (kind, name, endpoint, secret_name, subject)
select 'accounting', 'Comptabilité', p.acct_url, 'accounting', 'svc-accounting'
from p;

analyze;
