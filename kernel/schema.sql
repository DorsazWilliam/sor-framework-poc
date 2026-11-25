create function sor_now() returns timestamptz language sql stable as
$$ select coalesce(nullif(current_setting('sor.now', true), '')::timestamptz, transaction_timestamp()) $$;
comment on function sor_now() is 'The instant of the request: transaction timestamp, pinnable by the setting sor.now.';

create function sor_today() returns date language sql stable as $$ select sor_now()::date $$;
comment on function sor_today() is 'The date of sor_now() in the database time zone.';

create function sor_diff(o jsonb, n jsonb) returns jsonb language sql immutable as
$$ select coalesce(jsonb_object_agg(k, jsonb_build_array(o->k, n->k)), '{}'::jsonb)
   from jsonb_object_keys(coalesce(n, o)) k where (o->k) is distinct from (n->k) $$;
comment on function sor_diff(jsonb, jsonb) is 'Changed columns of a row: {column: [before, after]}.';

create function sor_append_only() returns trigger language plpgsql as
$$ begin
  raise exception '% is append-only', tg_table_name using errcode = '23514';
end $$;
comment on function sor_append_only() is 'Refuses update and delete on an append-only table.';

create type run_state as enum ('queued', 'running', 'done', 'cancelled', 'failed');
comment on type run_state is 'Lifecycle of a run.';

create table file (
    id bigint generated always as identity primary key,
    hash text not null unique check (hash ~ '^[0-9a-f]{64}$'),
    size bigint not null check (size >= 0),
    type text not null,
    name text not null,
    created_at timestamptz not null default sor_now()
);
comment on table file is 'A content-addressed blob of the object storage.';
comment on column file.hash is 'SHA-256 of the content, hex; the blob key.';
comment on column file.size is 'Size in bytes.';
comment on column file.type is 'Media type.';
comment on column file.name is 'File name.';
comment on column file.created_at is 'When the file was registered.';

create table event (
    id bigint generated always as identity,
    at timestamptz not null default sor_now(),
    call uuid not null,
    command text not null,
    principal_id bigint not null,
    for_id bigint,
    tbl text not null,
    row_id bigint not null,
    root_table text not null,
    root_id bigint not null,
    changes jsonb not null,
    primary key (id, at)
) partition by range (at);
comment on table event is 'One row per row changed by a command, written by the effect statement itself.';
comment on column event.at is 'Instant of the command.';
comment on column event.call is 'Groups the events of one command invocation.';
comment on column event.command is 'Command name.';
comment on column event.principal_id is 'Principal who ran the command.';
comment on column event.for_id is 'Human the principal acted for, if any.';
comment on column event.tbl is 'Table of the changed row.';
comment on column event.row_id is 'Id of the changed row.';
comment on column event.root_table is 'Root table of the changed row.';
comment on column event.root_id is 'Root id of the changed row.';
comment on column event.changes is 'Changed columns: {column: [before, after]}.';
create index event_root on event (root_table, root_id, at);
create index event_call on event (call);

create table run (
    id bigint generated always as identity primary key,
    name text not null,
    input jsonb not null default '{}',
    principal_id bigint not null,
    for_id bigint,
    state run_state not null default 'queued',
    total integer not null default 0,
    done integer not null default 0,
    rejected jsonb not null default '[]',
    error text,
    cancel_requested boolean not null default false,
    created_at timestamptz not null default sor_now(),
    finished_at timestamptz
);
comment on table run is 'A command executed over a set of roots, in one job and one transaction.';
comment on column run.name is 'Run name.';
comment on column run.input is 'Params of the candidates view.';
comment on column run.principal_id is 'Principal who started the run.';
comment on column run.for_id is 'Human the principal acted for, if any.';
comment on column run.state is 'Lifecycle state.';
comment on column run.total is 'Number of candidate roots.';
comment on column run.done is 'Number of roots processed.';
comment on column run.rejected is 'Roots refused: [{id, conditions, row}].';
comment on column run.error is 'Failure message.';
comment on column run.cancel_requested is 'Set by run.cancel; read by the run between two roots.';
comment on column run.created_at is 'When the run was queued.';
comment on column run.finished_at is 'When the run ended.';

create table saved_view (
    id bigint generated always as identity primary key,
    owner_id bigint not null,
    screen text not null,
    name text not null,
    state jsonb not null default '{}',
    unique (owner_id, screen, name)
);
comment on table saved_view is 'A named state (params, wrap, columns) of a screen, owned by a principal.';
comment on column saved_view.owner_id is 'Principal owning the saved view.';
comment on column saved_view.screen is 'Screen id.';
comment on column saved_view.name is 'Name.';
comment on column saved_view.state is 'Params, wrap and columns.';

create table connection (
    id bigint generated always as identity primary key,
    kind text not null,
    name text not null,
    endpoint text not null,
    secret_name text,
    principal_id bigint,
    cursor text,
    pulled_at timestamptz,
    pull_error text
);
comment on table connection is 'An external system.';
comment on column connection.kind is 'Kind of system.';
comment on column connection.name is 'Name.';
comment on column connection.endpoint is 'Base URL.';
comment on column connection.secret_name is 'Name of the secret in the vault.';
comment on column connection.principal_id is 'Principal acting for the connection.';
comment on column connection.cursor is 'Cursor of the last pull.';
comment on column connection.pulled_at is 'Instant of the last pull.';
comment on column connection.pull_error is 'Error of the last pull.';

create table delivery (
    id bigint generated always as identity primary key,
    connection_id bigint not null references connection,
    kind text not null,
    root_table text not null,
    root_id bigint not null,
    payload jsonb not null,
    principal_id bigint not null,
    created_at timestamptz not null default sor_now()
);
comment on table delivery is 'An outgoing message to an external system; sent by a job.';
comment on column delivery.connection_id is 'Target connection.';
comment on column delivery.kind is 'Kind of message.';
comment on column delivery.root_table is 'Root table of the delivered row.';
comment on column delivery.root_id is 'Root id of the delivered row.';
comment on column delivery.payload is 'Body sent to the endpoint.';
comment on column delivery.principal_id is 'Principal who caused the delivery.';
comment on column delivery.created_at is 'When the delivery was written.';
create index delivery_root on delivery (root_table, root_id);
create trigger delivery_append_only before update or delete on delivery
    for each row execute function sor_append_only();

create table delivery_result (
    id bigint generated always as identity primary key,
    delivery_id bigint not null references delivery,
    at timestamptz not null default sor_now(),
    ok boolean not null,
    status integer,
    detail text
);
comment on table delivery_result is 'Outcome of one delivery attempt.';
comment on column delivery_result.delivery_id is 'Delivery attempted.';
comment on column delivery_result.at is 'Instant of the attempt.';
comment on column delivery_result.ok is 'Whether the system accepted it.';
comment on column delivery_result.status is 'HTTP status.';
comment on column delivery_result.detail is 'Response or error detail.';
create index delivery_result_delivery on delivery_result (delivery_id);
create trigger delivery_result_append_only before update or delete on delivery_result
    for each row execute function sor_append_only();
