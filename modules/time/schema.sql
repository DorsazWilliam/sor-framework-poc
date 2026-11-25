create type expense_mode as enum ('both', 'user_only', 'billing_only');
comment on type expense_mode is 'Who an expense concerns: reimbursed to the user and billed, user only, or billing only.';

create table activity (
    id bigint generated always as identity primary key,
    user_id bigint not null references "user",
    date date not null,
    start_tod smallint not null check (start_tod between 0 and 1439),
    duration_min smallint not null check (duration_min > 0),
    billed_min smallint check (billed_min >= 0),
    folder_id bigint not null references folder,
    task_id bigint not null references task,
    description text not null default '',
    validated_by bigint references "user",
    validated_at timestamptz,
    constraint activity_end check (start_tod + duration_min <= 1440),
    constraint activity_validation check ((validated_by is null) = (validated_at is null))
);
comment on table activity is 'Time spent by a user on a task in a folder, on a date.';
comment on column activity.user_id is 'User who did the work.';
comment on column activity.date is 'Date of the work.';
comment on column activity.start_tod is 'Start, minute of the day.';
comment on column activity.duration_min is 'Real duration.';
comment on column activity.billed_min is 'Billed duration entered, for a task billed sometimes; empty = the real duration.';
comment on column activity.folder_id is 'Folder worked on.';
comment on column activity.task_id is 'Task done.';
comment on column activity.description is 'What was done.';
comment on column activity.validated_by is 'Manager who validated the activity.';
comment on column activity.validated_at is 'When the activity was validated.';
create index activity_user_date on activity (user_id, date) include (start_tod, duration_min, task_id);
create index activity_date on activity (date, id);
create index activity_folder on activity (folder_id, date) include (duration_min);

create table expense_type (
    id bigint generated always as identity primary key,
    name text not null,
    by_quantity boolean not null default false,
    unit_price numeric(10,2) check (unit_price >= 0),
    billing_unit_price numeric(10,2) check (billing_unit_price >= 0),
    mode expense_mode not null default 'both',
    archived boolean not null default false,
    constraint expense_type_prices check (not by_quantity or (unit_price is not null and billing_unit_price is not null))
);
comment on table expense_type is 'A type of expense, entered by amount or by quantity.';
comment on column expense_type.name is 'Name of the type.';
comment on column expense_type.by_quantity is 'Entered as a quantity priced by the type, instead of an amount.';
comment on column expense_type.unit_price is 'Price of one unit reimbursed to the user.';
comment on column expense_type.billing_unit_price is 'Price of one unit billed to the client.';
comment on column expense_type.mode is 'Default mode of its expenses.';
comment on column expense_type.archived is 'Archived types are hidden from choices.';

create table expense (
    id bigint generated always as identity primary key,
    activity_id bigint not null references activity,
    expense_type_id bigint not null references expense_type,
    quantity numeric(10,2) check (quantity >= 0),
    amount numeric(10,2) check (amount >= 0),
    mode expense_mode,
    receipt_file_id bigint references file,
    ocr_amount numeric(10,2)
);
comment on table expense is 'An expense line of an activity.';
comment on column expense.activity_id is 'Activity the expense belongs to.';
comment on column expense.expense_type_id is 'Type of expense.';
comment on column expense.quantity is 'Quantity, for a type by quantity.';
comment on column expense.amount is 'Amount paid, for a type by amount.';
comment on column expense.mode is 'Mode of the expense; empty = the mode of its type.';
comment on column expense.receipt_file_id is 'Receipt (scan or photo).';
comment on column expense.ocr_amount is 'Amount read on the receipt by OCR.';
create index expense_activity on expense (activity_id);

create table period_lock (
    id bigint generated always as identity primary key,
    company_id bigint not null default 1 references company,
    date_from date not null,
    date_to date not null,
    user_id bigint references "user",
    constraint period_lock_dates check (date_from <= date_to)
);
comment on table period_lock is 'A period in which no activity or expense can be written, for one user or all.';
comment on column period_lock.company_id is 'Company (always 1); locking a period locks the company row.';
comment on column period_lock.date_from is 'First day locked.';
comment on column period_lock.date_to is 'Last day locked.';
comment on column period_lock.user_id is 'User locked; empty = all users.';

create view lock_user as
select u.id as user_id, range_agg(daterange(l.date_from, l.date_to, '[]')) as dates
from "user" u
join period_lock l on l.user_id is null or l.user_id = u.id
group by u.id;
comment on view lock_user is 'Dates locked for each user: the locks of all users and the user''s own.';
comment on column lock_user.dates is 'Locked dates.';

create view folder_spent as
select f.id as folder_id, f.estimated_min,
       (select coalesce(sum(b.duration_min), 0) from activity b where b.folder_id = f.id) as spent_min
from folder f
where f.estimated_min is not null;
comment on view folder_spent is 'Real duration spent in each folder having an estimate.';
comment on column folder_spent.spent_min is 'Sum of the real durations of the folder.';
