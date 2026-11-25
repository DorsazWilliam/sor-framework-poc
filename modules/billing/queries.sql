-- name: InvoiceActivity :many
-- Activities of an invoice: attached once invoiced, candidates while draft.
-- @anchor none
-- @needs invoice
select a.*
from activity_p a
where a.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = @invoice_id))
order by a.date, a.start_tod, a.id;

-- name: InvoiceDocument :one
-- The JSON document of an invoice: company, client, project, period, lines, expenses, totals.
-- @anchor none
-- @needs invoice
select d.invoice_id, d.document
from invoice_document d
where d.invoice_id = @invoice_id;

-- name: InvoiceRunCandidates :many
-- Projects with billable activities over a period and no draft invoice overlapping it: the invoices a run creates.
-- @anchor none
-- @needs invoice
select f.project_id, @date_from::date as date_from, @date_to::date as date_to
from billable_activity b
join folder f on f.id = b.folder_id
where b.date between @date_from::date and @date_to::date
  and not exists (select from invoice i
                  where i.project_id = f.project_id and i.invoiced_at is null
                    and i.date_from <= @date_to::date and i.date_to >= @date_from::date)
group by f.project_id
order by f.project_id;

-- name: Profitability :many
-- Per folder and its project, over the activities of a period: durations, services, expenses billed and reimbursed,
-- realized (services and expenses billed), invoiced, balance (realized minus invoiced). Invoiced activities use the values of their invoice line.
-- @anchor none
-- @needs invoice
select f.project_id, r.*
from (
    select r.folder_id,
           sum(r.duration_min)::integer as duration_min, sum(r.billed_min)::integer as billed_min,
           sum(r.services_amount) as services_amount, sum(r.expense_billed_amount) as expense_billed_amount, sum(r.expense_user_amount) as expense_user_amount,
           sum(r.realized_amount) as realized_amount, sum(r.invoiced_amount) as invoiced_amount, sum(r.balance_amount) as balance_amount
    from activity_revenue r
    where r.date between @date_from and @date_to
    group by r.folder_id
) r
join folder f on f.id = r.folder_id;

-- name: ProfitabilityCost :many
-- Profitability with the cost (counted minutes times hourly cost) and the margins on services and on balance.
-- @anchor none
-- @needs cost
select f.project_id, r.*
from (
    select r.folder_id,
           sum(r.duration_min)::integer as duration_min, sum(r.billed_min)::integer as billed_min, sum(r.counted_min)::integer as counted_min,
           sum(r.services_amount) as services_amount, sum(r.expense_billed_amount) as expense_billed_amount, sum(r.expense_user_amount) as expense_user_amount,
           sum(r.realized_amount) as realized_amount, sum(r.invoiced_amount) as invoiced_amount, sum(r.balance_amount) as balance_amount,
           sum(r.cost_amount) as cost_amount, sum(r.services_margin_amount) as services_margin_amount, sum(r.balance_margin_amount) as balance_margin_amount
    from activity_cost r
    where r.date between @date_from and @date_to
    group by r.folder_id
) r
join folder f on f.id = r.folder_id;

-- name: InvoiceLockActivities :exec
-- Locks the candidate activities of draft invoices, by id: the following statements write the lines of these only.
select a.id from activity a
where a.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = any(@ids::bigint[])))
order by a.id
for update;

-- name: InvoiceWriteLines :exec
-- Writes a line per locked activity on its invoice, with rate, billed duration and amount read in its projection.
insert into invoice_line (invoice_id, activity_id, rate, billed_min, amount)
select i.id, p.id, p.rate, p.billed_min, p.amount
from activity_p p
join invoice i on i.project_id = p.project_id and p.date between i.date_from and i.date_to and i.id = any(@ids::bigint[])
where p.id = any(@locked::bigint[]);

-- name: InvoiceWriteExpenses :exec
-- Writes a line per expense of the locked activities on the invoice of its activity, with the amounts read in its projection.
insert into invoice_expense (invoice_id, expense_id, user_amount, billed_amount)
select l.invoice_id, x.id, x.user_amount, x.billed_amount
from expense_p x
join invoice_line l on l.activity_id = x.activity_id
where x.activity_id = any(@locked::bigint[]);

-- name: InvoiceMarkInvoiced :exec
-- Marks invoices invoiced by the principal, now.
update invoice set invoiced_at = sor_now(), invoiced_by = @self where id = any(@ids::bigint[]);

-- name: InvoiceDeleteExpenses :exec
-- Deletes the expense lines of invoices.
delete from invoice_expense where invoice_id = any(@ids::bigint[]);

-- name: InvoiceDeleteLines :exec
-- Deletes the activity lines of invoices.
delete from invoice_line where invoice_id = any(@ids::bigint[]);

-- name: InvoiceBackToDraft :exec
-- Returns invoices to draft.
update invoice set invoiced_at = null, invoiced_by = null where id = any(@ids::bigint[]);

-- name: InvoiceAttachPdf :exec
-- Attaches the rendered PDF to invoices.
update invoice set pdf_file_id = @pdf_file_id where id = any(@ids::bigint[]);
