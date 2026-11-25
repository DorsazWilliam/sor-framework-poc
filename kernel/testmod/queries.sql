-- name: Principal :many
-- @anchor none
select p.subject, p.id, p.kind, p.name, p.lang, p.functions,
       jsonb_build_object(
         'team', coalesce((select jsonb_agg(t.id) from person t where t.manager_id = p.id), '[]'::jsonb),
         'user', jsonb_build_array(p.id) || coalesce((select jsonb_agg(t.id) from person t where t.manager_id = p.id), '[]'::jsonb)
       ) as reach
from person p
where p.subject = any(@subjects::text[]);

-- name: EntryGrid :many
-- Entries with the name of their person.
-- @anchor person_id user
-- @needs entry
select e.id, e.person_id, p.name as person, e.day, e.worked_min, e.validated, e.not_locked
from entry_p e
join person p on p.id = e.person_id
where e.person_id = any(@reach::bigint[])
  and e.day >= coalesce(sqlc.narg(day_from)::date, '-infinity'::date);

-- name: EntryLockRows :exec
-- Locks the entries.
select id from entry where id = any(@ids::bigint[]) order by id for update;

-- name: EntryValidate :exec
-- Marks the locked entries validated.
update entry set validated = true where id = any(@locked::bigint[]);

-- name: EntryRunCandidates :many
-- Entries a run creates on day: n of them, for person 1 before index split and person 2 after; index bad is too long.
-- @anchor none
-- @needs admin
select (case when i < @split::int then 1 else 2 end)::bigint as person_id, @day::date as "day",
       (case when i = @bad::int then 700 else 15 end) as worked_min
from generate_series(0, 7) i
where i < @n::int;

-- name: EntryExport :exec
-- Sends the entries to connection 1.
insert into delivery (connection_id, kind, root_table, root_id, payload, principal_id)
select 1, 'invoice', 'entry', e.id, jsonb_build_object('id', e.id, 'note', e.note), @self::bigint
from entry e where e.id = any(@ids::bigint[]);
