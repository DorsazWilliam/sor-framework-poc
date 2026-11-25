-- The accounting module adds no table: it works on the kernel's connection and delivery.

create function csv_field(t text) returns text language sql immutable parallel safe as
$$ select case when t ~ '[;"\n\r]' then '"' || replace(t, '"', '""') || '"' else coalesce(t, '') end $$;
comment on function csv_field(text) is 'A CSV field separated by ;: quoted, its quotes doubled, when it contains ;, a quote or a line break; empty for null.';

create view invoice_csv as
select i.id as invoice_id,
       'invoice;account;project;date;user;task;description;minutes;rate;amount' || E'\n'
       || coalesce((select string_agg(array_to_string(array[i.id::text, csv_field(a.external_id), csv_field(p.external_id), l.date::text,
                                                            csv_field(l.user_name), csv_field(l.task), csv_field(l.description),
                                                            l.minutes::text, l.rate::text, l.amount::text], ';', ''), E'\n' order by l.date, l.expense, l.id) || E'\n'
                    from (select x.id, false as expense, x.date, u.name as user_name, t.name as task, x.description,
                                 x.billed_min as minutes, x.rate, x.amount
                          from activity_p x
                          join "user" u on u.id = x.user_id
                          join task t on t.id = x.task_id
                          where x.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = i.id))
                          union all
                          select x.id, true, y.date, u.name, et.name, null, null, null, x.billed_amount
                          from expense_p x
                          join activity y on y.id = x.activity_id
                          join "user" u on u.id = y.user_id
                          join expense_type et on et.id = x.expense_type_id
                          where x.activity_id = any(array(select l.activity_id from invoice_item l where l.invoice_id = i.id))) l), '') as body
from invoice i
left join project p on p.id = i.project_id
left join account a on a.id = p.account_id;
comment on view invoice_csv is 'The export of an invoice to the accounting system, as CSV (separator ;, decimal point, ISO dates): a header, then a line per activity of the invoice and per expense of them.';
comment on column invoice_csv.invoice_id is 'Invoice.';
comment on column invoice_csv.body is 'The CSV.';
