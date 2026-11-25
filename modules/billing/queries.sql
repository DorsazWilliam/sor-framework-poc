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
-- realized (services and expenses billed), invoiced, balance (realized minus invoiced). Invoiced activities use their frozen values.
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
-- Locks the candidate activities of draft invoices, by id: the following statements stamp these only.
select a.id from activity a
where a.id = any(array(select l.activity_id from invoice_item l where l.invoice_id = any(@ids::bigint[])))
order by a.id
for update;

-- name: InvoiceStampActivities :exec
-- Stamps each locked activity with its invoice and the rate, billed duration and amount of its projection.
update activity a set invoice_id = i.id,
       (frozen_rate, frozen_billed_min, frozen_amount) = (select p.rate, p.billed_min, p.amount from activity_p p where p.id = a.id)
from invoice i
join folder f on f.project_id = i.project_id
where i.id = any(@ids::bigint[]) and a.folder_id = f.id and a.date between i.date_from and i.date_to and a.id = any(@locked::bigint[]);

-- name: InvoiceStampExpenses :exec
-- Stamps the expenses of the locked activities with the amounts of their projection.
update expense e set (frozen_user_amount, frozen_billed_amount) = (select x.user_amount, x.billed_amount from expense_p x where x.id = e.id)
where e.activity_id = any(@locked::bigint[]);

-- name: InvoiceMarkInvoiced :exec
-- Marks invoices invoiced by the principal, now.
update invoice set invoiced_at = sor_now(), invoiced_by = @self where id = any(@ids::bigint[]);

-- name: InvoiceClearExpenses :exec
-- Clears the stamps of the expenses of the activities of invoices.
update expense set frozen_user_amount = null, frozen_billed_amount = null
where activity_id = any(array(select a.id from activity a where a.invoice_id = any(@ids::bigint[])));

-- name: InvoiceClearActivities :exec
-- Clears the stamps of the activities of invoices.
update activity set invoice_id = null, frozen_rate = null, frozen_billed_min = null, frozen_amount = null
where invoice_id = any(@ids::bigint[]);

-- name: InvoiceBackToDraft :exec
-- Returns invoices to draft.
update invoice set invoiced_at = null, invoiced_by = null where id = any(@ids::bigint[]);

-- name: InvoiceAttachPdf :exec
-- Attaches the rendered PDF to invoices.
update invoice set pdf_file_id = @pdf_file_id where id = any(@ids::bigint[]);
