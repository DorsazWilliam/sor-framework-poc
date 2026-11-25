create type fn as enum ('entry', 'admin', 'validate');
create type person_kind as enum ('human', 'agent', 'service');

create table org (
    id bigint generated always as identity primary key,
    name text not null
);
comment on table org is 'The single organisation; upper root of entries.';

create table person (
    id bigint generated always as identity primary key,
    kind person_kind not null default 'human',
    subject text not null unique,
    name text not null,
    lang text not null default 'fr',
    manager_id bigint references person,
    functions fn[] not null default '{}'
);
comment on table person is 'A principal.';
comment on column person.name is 'Display name.';

create table org_lock (
    id bigint generated always as identity primary key,
    org_id bigint not null default 1 references org,
    date_from date not null,
    date_to date not null,
    check (date_from <= date_to)
);
comment on table org_lock is 'A locked period; child of org.';

create table entry (
    id bigint generated always as identity primary key,
    person_id bigint not null references person,
    day date not null,
    worked_min integer not null check (worked_min > 0),
    note text,
    validated boolean not null default false,
    file_id bigint references file
);
comment on table entry is 'Time entry.';

create table entry_line (
    id bigint generated always as identity primary key,
    entry_id bigint not null references entry,
    label text not null,
    line_min integer not null default 0
);
comment on table entry_line is 'Detail line of an entry.';

create view entry_p as
select e.*,
       not exists (select 1 from org_lock l where e.day between l.date_from and l.date_to) as not_locked,
       not e.validated as not_validated,
       e.worked_min <= 600 as short,
       coalesce((select sum(l.line_min) from entry_line l where l.entry_id = e.id), 0)::integer as lines_min
from entry e;
comment on view entry_p is 'Projection of entry.';
