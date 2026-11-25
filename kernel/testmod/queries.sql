-- name: Principal :many
-- @anchor none
select p.subject, p.id, p.kind, p.name, p.lang, p.functions,
       jsonb_build_object(
         'self', jsonb_build_array(p.id),
         'team', coalesce((select jsonb_agg(t.id) from person t where t.manager_id = p.id), '[]'::jsonb),
         'user', jsonb_build_array(p.id) || coalesce((select jsonb_agg(t.id) from person t where t.manager_id = p.id), '[]'::jsonb)
       ) as reach
from person p
where p.subject = any(@subjects::text[]);

-- name: EntryGrid :many
-- Entries with the name of their person.
-- @anchor person_id user
select e.id, e.person_id, p.name as person, e.day, e.worked_min, e.validated, e.not_locked
from entry_p e
join person p on p.id = e.person_id
where e.person_id = any(@reach::bigint[])
  and e.day >= coalesce(sqlc.narg(day_from)::date, '-infinity'::date);

-- name: EntryLockRows :exec
-- Locks the entries.
select id from entry where id = any(@ids::bigint[]) order by id for update;

-- name: EntryValidate :exec
-- Marks the entries validated.
update entry set validated = true where id = any(@ids::bigint[]);
