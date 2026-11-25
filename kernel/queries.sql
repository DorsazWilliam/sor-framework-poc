-- name: History :many
-- Events of a root, newest first. Served after the root is read through its list view.
-- @anchor none
select e.at, e.command, e.principal_id as principal, e.for_id as "for", e.tbl, e.row_id, e.changes
from event e
where e.root_table = @root_table and e.root_id = @root_id
order by e.at desc, e.id desc;

-- name: RunCancel :exec
-- Asks a run to stop between two roots.
update run set cancel_requested = true where id = any(@ids::bigint[]);

-- name: DeliveryResult :exec
-- Records the outcome of a delivery attempt.
insert into delivery_result (delivery_id, ok, status, detail)
select d.id, @ok::boolean, sqlc.narg(status)::integer, sqlc.narg(detail)::text from delivery d where d.id = any(@ids::bigint[]);
