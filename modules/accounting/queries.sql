-- name: ConnectionDelivery :many
-- Deliveries to a connection, newest first, with their state.
-- @anchor none
-- @needs master_data
select s.delivery_id as id, s.root_table, s.root_id, s.created_at, s.state, s.attempts, s.last_at, s.last_status, s.last_detail
from delivery_state s
where s.connection_id = @connection_id
order by s.delivery_id desc;

-- name: AccountingExport :many
-- Exports of an invoice to the accounting system: state, attempts and last detail.
-- @anchor none
-- @needs invoice
select s.delivery_id as id, s.connection_id, s.created_at, s.state, s.attempts, s.last_at, s.last_status, s.last_detail
from delivery_state s
where s.root_table = 'invoice' and s.root_id = @invoice_id
order by s.delivery_id desc;

-- name: InvoiceExport :exec
-- Writes the delivery of the CSV of each invoice to /invoices of the accounting connection.
insert into delivery (connection_id, path, content_type, body, root_table, root_id, principal_id)
select c.id, '/invoices', 'text/csv; charset=utf-8', x.body, 'invoice', x.invoice_id, @self
from invoice_csv x
join connection c on c.kind = 'accounting'
where x.invoice_id = any(@ids::bigint[]);

-- name: ConnectionAdvance :exec
-- Records a pull of connections: its cursor (kept when empty), the instant and the error if any.
update connection set cursor = coalesce(sqlc.narg(cursor)::text, cursor), pulled_at = sor_now(), pull_error = sqlc.narg(pull_error)::text
where id = any(@ids::bigint[]);

-- name: AccountUpsertFromAccounting :exec
-- Creates or updates a client from the accounting system, by its external id.
insert into account (external_id, name, address) values (@external_id, @name, @address)
on conflict (external_id) do update set name = excluded.name, address = excluded.address;

-- name: ProjectUpsertFromAccounting :exec
-- Creates or updates a project from the accounting system, by its external id and the external id of its client.
insert into project (external_id, account_id, name)
select @external_id, a.id, @name from account a where a.external_id = @account_external_id
on conflict (external_id) do update set name = excluded.name, account_id = excluded.account_id;
