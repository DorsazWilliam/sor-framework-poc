create type fn as enum ('activity', 'expense', 'invoice', 'invoice_state', 'period_lock', 'validation', 'cost',
    'master_data', 'scope_direct', 'scope_tree', 'scope_all', 'accounting_sync', 'ocr');
comment on type fn is 'A function (right) granted by a group.';
create type principal_kind as enum ('human', 'agent', 'service');
comment on type principal_kind is 'Kind of principal.';
create type lang as enum ('fr', 'de', 'en');
comment on type lang is 'UI language.';
create type task_kind as enum ('service', 'absence', 'vacation');
comment on type task_kind is 'Kind of task: a service rendered, an absence, or vacation.';
create type billing_mode as enum ('always', 'sometimes', 'never');
comment on type billing_mode is 'How a task is billed: always the real duration, sometimes an entered duration, never.';

create table company (
    id bigint generated always as identity primary key check (id = 1),
    name text not null,
    address text not null default '',
    vat_no text not null default '',
    currency text not null default 'CHF' check (currency ~ '^[A-Z]{3}$')
);
comment on table company is 'The company using the system; a single row (id 1).';
comment on column company.name is 'Company name.';
comment on column company.address is 'Postal address, one line per line.';
comment on column company.vat_no is 'VAT number.';
comment on column company.currency is 'ISO 4217 currency of all amounts.';

create table "user" (
    id bigint generated always as identity primary key,
    kind principal_kind not null default 'human',
    subject text not null unique,
    name text not null,
    manager_id bigint references "user",
    lang lang not null default 'fr',
    archived boolean not null default false,
    check (manager_id <> id)
);
comment on table "user" is 'A principal: a human, an agent acting for humans, or a service.';
comment on column "user".kind is 'Human, agent or service.';
comment on column "user".subject is 'Subject (sub) of the identity provider.';
comment on column "user".name is 'Display name.';
comment on column "user".manager_id is 'Manager of a human; validates the activities of the team.';
comment on column "user".lang is 'UI language.';
comment on column "user".archived is 'Archived principals cannot sign in.';
create index user_manager on "user" (manager_id);

create table user_group (
    id bigint generated always as identity primary key,
    name text not null unique,
    admin boolean not null default false
);
comment on table user_group is 'A group of principals; its functions are granted to its members.';
comment on column user_group.name is 'Group name.';
comment on column user_group.admin is 'Members have every function.';

create table user_group_member (
    id bigint generated always as identity primary key,
    user_group_id bigint not null references user_group,
    user_id bigint not null references "user",
    unique (user_group_id, user_id)
);
comment on table user_group_member is 'Membership of a principal in a group.';
comment on column user_group_member.user_group_id is 'Group.';
comment on column user_group_member.user_id is 'Member.';
create index user_group_member_user on user_group_member (user_id);

create table user_group_fn (
    id bigint generated always as identity primary key,
    user_group_id bigint not null references user_group,
    fn fn not null,
    unique (user_group_id, fn)
);
comment on table user_group_fn is 'A function granted to the members of a group.';
comment on column user_group_fn.user_group_id is 'Group.';
comment on column user_group_fn.fn is 'Function granted.';

create table account (
    id bigint generated always as identity primary key,
    external_id text not null unique,
    name text not null,
    address text not null default '',
    archived boolean not null default false
);
comment on table account is 'A client; imported from the accounting system.';
comment on column account.external_id is 'Id in the accounting system.';
comment on column account.name is 'Client name.';
comment on column account.address is 'Billing address, one line per line.';
comment on column account.archived is 'Archived clients are hidden from choices.';

create table project (
    id bigint generated always as identity primary key,
    external_id text not null unique,
    account_id bigint not null references account,
    name text not null,
    rate numeric(10,2) check (rate >= 0),
    entry_closed boolean not null default false,
    archived boolean not null default false,
    lead_user_id bigint references "user"
);
comment on table project is 'A project of a client; activities are entered in its folders.';
comment on column project.external_id is 'Id in the accounting system.';
comment on column project.account_id is 'Client billed for the project.';
comment on column project.name is 'Project name.';
comment on column project.rate is 'Hourly rate of the project, used when the folder has none.';
comment on column project.entry_closed is 'No activity can be entered in the project.';
comment on column project.archived is 'Archived projects accept no activity.';
comment on column project.lead_user_id is 'User responsible for the project.';
create index project_account on project (account_id);

create table folder (
    id bigint generated always as identity primary key,
    project_id bigint not null references project,
    parent_id bigint,
    name text not null,
    rate numeric(10,2) check (rate >= 0),
    estimated_min integer check (estimated_min >= 0),
    entry_closed boolean not null default false,
    unique (project_id, id),
    foreign key (project_id, parent_id) references folder (project_id, id),
    check (parent_id <> id)
);
comment on table folder is 'A node of the folder tree of a project; activities are entered on a folder.';
comment on column folder.project_id is 'Project of the folder.';
comment on column folder.parent_id is 'Parent folder, in the same project.';
comment on column folder.name is 'Folder name.';
comment on column folder.rate is 'Hourly rate of the folder; its own value, not inherited.';
comment on column folder.estimated_min is 'Estimated duration; activities of a folder whose real total exceeds it must be validated.';
comment on column folder.entry_closed is 'No activity can be entered in the folder.';

create view folder_p as
select f.*, not f.entry_closed and not p.entry_closed and not p.archived as entry_open
from folder f
left join project p on p.id = f.project_id;
comment on view folder_p is 'Projection of folder.';
comment on column folder_p.entry_open is 'Activities can be entered: the folder and its project are open and the project is not archived.';

create table task (
    id bigint generated always as identity primary key,
    name text not null,
    kind task_kind not null default 'service',
    billing billing_mode not null default 'always',
    requires_validation boolean not null default false,
    counts_hours boolean not null default true,
    rate numeric(10,2) check (rate >= 0),
    archived boolean not null default false
);
comment on table task is 'A type of task, from a global catalog, allowed folder by folder.';
comment on column task.name is 'Task name.';
comment on column task.kind is 'Service, absence or vacation.';
comment on column task.billing is 'Billed duration: always the real duration, sometimes an entered duration, never.';
comment on column task.requires_validation is 'Its activities must be validated by the manager.';
comment on column task.counts_hours is 'Its activities count in the hours done.';
comment on column task.rate is 'Hourly rate of the task, used when neither the folder nor the project has one.';
comment on column task.archived is 'Archived tasks are hidden from choices.';

create table folder_task (
    id bigint generated always as identity primary key,
    folder_id bigint not null references folder,
    task_id bigint not null references task,
    unique (folder_id, task_id)
);
comment on table folder_task is 'A task allowed in a folder.';
comment on column folder_task.folder_id is 'Folder.';
comment on column folder_task.task_id is 'Task allowed.';

create table user_task_rate (
    id bigint generated always as identity primary key,
    user_id bigint not null references "user",
    task_id bigint not null references task,
    rate numeric(10,2) not null check (rate >= 0),
    unique (user_id, task_id)
);
comment on table user_task_rate is 'Hourly rate of a user for a task; replaces every other rate.';
comment on column user_task_rate.user_id is 'User.';
comment on column user_task_rate.task_id is 'Task.';
comment on column user_task_rate.rate is 'Hourly rate.';

create table work_period (
    id bigint generated always as identity primary key,
    user_id bigint not null references "user",
    valid_from date not null,
    week_min integer[] not null check (array_length(week_min, 1) = 7 and 0 <= all (week_min) and 1440 >= all (week_min)),
    vacation_days numeric(5,2) not null check (vacation_days >= 0),
    activity_pct numeric(5,2) not null default 100 check (activity_pct > 0 and activity_pct <= 100),
    unique (user_id, valid_from)
);
comment on table work_period is 'Working terms of a user from a date until the next period.';
comment on column work_period.user_id is 'User.';
comment on column work_period.valid_from is 'First day of the period.';
comment on column work_period.week_min is 'Full-time minutes per ISO weekday, Monday to Sunday.';
comment on column work_period.vacation_days is 'Vacation days per full year.';
comment on column work_period.activity_pct is 'Activity rate applied to the minutes due and the vacation.';

create table cost_rate (
    id bigint generated always as identity primary key,
    user_id bigint not null references "user",
    valid_from date not null,
    hourly_cost numeric(10,2) not null check (hourly_cost >= 0),
    unique (user_id, valid_from)
);
comment on table cost_rate is 'Hourly cost of a user from a date until the next one. Sensitive.';
comment on column cost_rate.user_id is 'User.';
comment on column cost_rate.valid_from is 'First day of the cost.';
comment on column cost_rate.hourly_cost is 'Cost of one hour of the user.';

create table public_holiday (
    id bigint generated always as identity primary key,
    date date not null unique,
    label text not null,
    deducted_min integer check (deducted_min > 0 and deducted_min <= 1440)
);
comment on table public_holiday is 'A public holiday, full or partial.';
comment on column public_holiday.date is 'Date.';
comment on column public_holiday.label is 'Name of the holiday.';
comment on column public_holiday.deducted_min is 'Minutes deducted from the day; empty = the whole day.';

create table hour_class (
    id bigint generated always as identity primary key,
    label text not null,
    hours_coef numeric(4,2) not null default 1 check (hours_coef >= 0),
    rate_coef numeric(4,2) not null default 1 check (rate_coef >= 0),
    requires_validation boolean not null default false
);
comment on table hour_class is 'A class of hours (night, week-end) weighting the hours counted and the rate.';
comment on column hour_class.label is 'Name of the class.';
comment on column hour_class.hours_coef is 'Coefficient on the minutes counted.';
comment on column hour_class.rate_coef is 'Coefficient on the rate.';
comment on column hour_class.requires_validation is 'Activities touching the class must be validated by the manager.';

create table hour_class_slot (
    id bigint generated always as identity primary key,
    hour_class_id bigint not null references hour_class,
    day_type smallint not null check (day_type between 1 and 8),
    span_tod int4range not null check (not isempty(span_tod) and lower(span_tod) >= 0 and upper(span_tod) <= 1440),
    constraint hour_class_slot_overlap exclude using gist (day_type with =, span_tod with &&)
);
comment on table hour_class_slot is 'A time slot of an hour class; a minute belongs to one class at most.';
comment on column hour_class_slot.hour_class_id is 'Hour class.';
comment on column hour_class_slot.day_type is 'ISO weekday 1 to 7, or 8 for a full public holiday.';
comment on column hour_class_slot.span_tod is 'Minutes of the day covered, half-open.';
create index hour_class_slot_class on hour_class_slot (hour_class_id);

create view day_profile as
select s.day_type,
       range_agg(s.span_tod) as classed,
       coalesce(range_agg(s.span_tod) filter (where c.requires_validation), '{}') as validating,
       array_agg(s.span_tod order by s.span_tod) as slots,
       array_agg(c.rate_coef - 1 order by s.span_tod) as rate_extra,
       array_agg(c.hours_coef - 1 order by s.span_tod) as hours_extra
from hour_class_slot s
join hour_class c on c.id = s.hour_class_id
group by s.day_type;
comment on view day_profile is 'Per day type: minutes covered by an hour class, by a class requiring validation, and the slots with their coefficients.';
comment on column day_profile.day_type is 'ISO weekday 1 to 7, or 8 for a full public holiday.';
comment on column day_profile.classed is 'Minutes of the day in some hour class.';
comment on column day_profile.validating is 'Minutes of the day in a class requiring validation.';
comment on column day_profile.slots is 'Slots of the day, in order.';
comment on column day_profile.rate_extra is 'Rate coefficient minus one of each slot.';
comment on column day_profile.hours_extra is 'Hours coefficient minus one of each slot.';

create function hour_class_extra(span int4range, slots int4range[], extra numeric[]) returns numeric
language plpgsql immutable parallel safe as $$
declare
  r numeric := 0;
begin
  for i in 1 .. coalesce(array_length(slots, 1), 0) loop
    if slots[i] && span then
      r := r + (upper(slots[i] * span) - lower(slots[i] * span)) * extra[i];
    end if;
  end loop;
  return r;
end $$;
comment on function hour_class_extra(int4range, int4range[], numeric[]) is 'Extra weighted minutes of a span: its overlap with each slot times the slot''s coefficient minus one.';

create view work_period_span as
select w.id, w.user_id, w.valid_from, coalesce(min(n.valid_from), 'infinity') as valid_to,
       w.week_min, w.vacation_days, w.activity_pct
from work_period w
left join work_period n on n.user_id = w.user_id and n.valid_from > w.valid_from
group by w.id, w.user_id;
comment on view work_period_span is 'Work periods with their end: the start of the next period of the user (excluded).';
comment on column work_period_span.valid_to is 'First day after the period.';

create view work_start as
select w.user_id, min(w.valid_from) as first_day
from work_period w
group by w.user_id;
comment on view work_start is 'First day of the first work period of each user: where balances start.';
comment on column work_start.first_day is 'First day worked.';

-- Its plan does not depend on the arguments: generic, so that it is not planned again at every call.
create function work_due(user_id bigint, date_from date, date_to date, out due_min numeric, out vacation_min numeric)
language sql stable parallel safe set plan_cache_mode = force_generic_plan as $$
  select coalesce(sum((d.week_total - d.holiday_total) * s.activity_pct / 100), 0),
         coalesce(sum((s.e - s.s + 1)::numeric / s.year_days * s.vacation_days * s.week_sum / 5 * s.activity_pct / 100), 0)
  from (
    select w.week_min, w.vacation_days, w.activity_pct,
           (select sum(m) from unnest(w.week_min) m) as week_sum,
           greatest(w.valid_from, make_date(y, 1, 1), work_due.date_from) as s,
           least(w.valid_to - 1, make_date(y, 12, 31), work_due.date_to) as e,
           make_date(y + 1, 1, 1) - make_date(y, 1, 1) as year_days
    from work_period_span w
    cross join generate_series(extract(year from greatest(w.valid_from, work_due.date_from))::integer,
                               extract(year from least(w.valid_to - 1, work_due.date_to))::integer) y
    where w.user_id = work_due.user_id and w.valid_from <= work_due.date_to and w.valid_to > work_due.date_from
  ) s
  cross join lateral (
    select (select sum(s.week_min[k] * ((s.e - s.s + 1) / 7 + case when (k - extract(isodow from s.s)::integer + 7) % 7 < (s.e - s.s + 1) % 7 then 1 else 0 end))
            from generate_series(1, 7) k) as week_total,
           (select coalesce(sum(least(coalesce(h.deducted_min, s.week_min[extract(isodow from h.date)::integer]), s.week_min[extract(isodow from h.date)::integer])), 0)
            from public_holiday h where h.date between s.s and s.e) as holiday_total
  ) d
  where s.s <= s.e $$;
comment on function work_due(bigint, date, date) is 'Minutes due and vacation minutes earned by a user over [date_from, date_to]: weekdays counted in closed form per work period and year, public holidays deducted, prorated by the activity rate.';

create view cost_rate_span as
select c.id, c.user_id, c.valid_from, coalesce(min(n.valid_from), 'infinity') as valid_to, c.hourly_cost
from cost_rate c
left join cost_rate n on n.user_id = c.user_id and n.valid_from > c.valid_from
group by c.id, c.user_id;
comment on view cost_rate_span is 'Cost rates with their end: the start of the next cost rate of the user (excluded). Sensitive.';
comment on column cost_rate_span.valid_to is 'First day after the cost rate.';
