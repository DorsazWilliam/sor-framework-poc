-- name: Principal :many
-- Principals of the given identity-provider subjects: id, kind, name, language, functions
-- (union of their groups; an admin group grants all) and named reaches: self = the principal,
-- team = direct reports (scope_direct), the whole subtree (scope_tree) or every other human (scope_all),
-- user = self and team. Archived principals are absent.
-- @anchor none
-- @needs master_data
with recursive p as (
    select u.id, u.kind, u.name, u.lang,
           case when bool_or(g.admin) then enum_range(null::fn)
                else coalesce(array_agg(distinct gf.fn) filter (where gf.fn is not null), '{}') end as functions
    from "user" u
    left join user_group_member m on m.user_id = u.id
    left join user_group g on g.id = m.user_group_id
    left join user_group_fn gf on gf.user_group_id = g.id
    where u.subject = any(@subjects::text[]) and not u.archived
    group by u.id
), tree as (
    select p.id as root_id, u.id from p join "user" u on u.manager_id = p.id
    where 'scope_tree' = any(p.functions)
    union
    select t.root_id, u.id from tree t join "user" u on u.manager_id = t.id
)
select p.id, p.kind, p.name, p.lang, p.functions,
       jsonb_build_object('self', jsonb_build_array(p.id), 'team', to_jsonb(t.ids), 'user', to_jsonb(p.id || t.ids)) as reach
from p
cross join lateral (
    select coalesce(array_agg(u.id order by u.id), '{}') as ids
    from "user" u
    where u.id <> p.id
      and ('scope_all' = any(p.functions) and u.kind = 'human'
           or 'scope_direct' = any(p.functions) and u.manager_id = p.id
           or u.id in (select tree.id from tree where tree.root_id = p.id))
) t;

-- name: UserLookup :many
-- Principals for choices: name, kind and whether archived.
-- @anchor none
select u.id, u.name as label, u.kind, u.archived
from "user" u
order by u.name;

-- name: ProjectLookup :many
-- Projects for choices, labelled "client / project".
-- @anchor none
select p.id, a.name || ' / ' || p.name as label, p.account_id, p.entry_closed or p.archived as closed
from project p
join account a on a.id = p.account_id
order by label;

-- name: FolderLookup :many
-- Folders for choices, labelled "project / folder path", with whether activities can be entered.
-- @anchor none
with recursive tree as (
    select f.id, p.name || ' / ' || f.name as label
    from folder f join project p on p.id = f.project_id
    where f.parent_id is null
    union all
    select f.id, t.label || ' / ' || f.name
    from tree t join folder f on f.parent_id = t.id
)
select t.id, t.label, f.project_id, f.entry_open
from tree t
join folder_p f on f.id = t.id
order by t.label;

-- name: FolderTaskAllowed :many
-- Tasks allowed in a folder, not archived.
-- @anchor none
select t.id, t.name, t.kind, t.billing, t.requires_validation, t.counts_hours
from folder_task ft
join task t on t.id = ft.task_id
where ft.folder_id = @folder_id and not t.archived
order by t.name;
