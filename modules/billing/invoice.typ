// Invoice and recap of its activities. Reads the invoice document (view invoice_document) from data.json.
// Built-in fonts only: compile with --ignore-system-fonts --creation-timestamp <epoch of the document>.
#let d = json("data.json")
#let cur = d.company.currency

#let pad2(n) = if n < 10 { "0" + str(n) } else { str(n) }
#let money(x) = {
  let c = int(calc.round(calc.abs(x) * 100))
  let u = str(calc.quo(c, 100))
  let groups = ()
  while u.len() > 3 {
    groups.insert(0, u.slice(u.len() - 3))
    u = u.slice(0, u.len() - 3)
  }
  groups.insert(0, u)
  (if x < 0 { "−" } else { "" }) + groups.join("’") + "." + pad2(calc.rem(c, 100))
}
#let hm(m) = str(calc.quo(m, 60)) + ":" + pad2(calc.rem(m, 60))
#let day(s) = s.split("-").rev().join(".")
#let lines(s) = s.split("\n").join(linebreak())

#set document(title: "Facture " + str(d.invoice.id), author: d.company.name)
#set text(font: "Libertinus Serif", size: 10pt, lang: "fr")
#set page(
  paper: "a4",
  margin: (x: 20mm, top: 18mm, bottom: 22mm),
  footer: context [
    #set text(size: 8pt, fill: luma(90))
    #d.company.name #h(1fr) Facture #d.invoice.id #h(1fr) Page #counter(page).display() / #counter(page).final().first()
  ],
)
#set table(stroke: none, inset: (x: 4pt, y: 3pt))
#show table.cell.where(y: 0): set text(weight: "bold")

#grid(
  columns: (1fr, 1fr),
  [
    #text(size: 13pt, weight: "bold", d.company.name) \
    #lines(d.company.address) \
    #if d.company.vat_no != "" [#d.company.vat_no]
  ],
  align(right, block(width: 75mm, inset: (top: 14mm), align(left)[
    #text(weight: "bold", d.account.name) \
    #lines(d.account.address)
  ])),
)

#v(12mm)
#text(size: 16pt, weight: "bold")[
  #if d.invoice.state == "draft" [Facture n° #d.invoice.id — en cours] else [Facture n° #d.invoice.id]
]
#v(2mm)
#grid(
  columns: (32mm, 1fr),
  row-gutter: 4pt,
  [Date], [#if d.invoice.invoiced_on != none { day(d.invoice.invoiced_on) } else [—]],
  [Client], [#d.account.name (#d.account.external_id)],
  [Projet], [#d.project.name (#d.project.external_id)],
  [Période], [du #day(d.invoice.date_from) au #day(d.invoice.date_to)],
)

#v(8mm)
#table(
  columns: (1fr, 25mm, 35mm),
  align: (left, right, right),
  table.hline(stroke: 0.6pt),
  table.header([Désignation], [Durée], [Montant #cur]),
  table.hline(stroke: 0.4pt),
  [Prestations selon récapitulatif], [#hm(d.totals.minutes)], [#money(d.totals.services)],
  [Frais], [], [#money(d.totals.expenses)],
  table.hline(stroke: 0.6pt),
  table.cell(colspan: 2)[*Total #cur*], [*#money(d.totals.total)*],
  table.hline(stroke: 0.6pt),
)

#v(10mm)
#text(size: 12pt, weight: "bold")[Récapitulatif des activités]
#v(2mm)
#set text(size: 8.5pt)
#table(
  columns: (17mm, 28mm, 28mm, 1fr, 13mm, 15mm, 20mm),
  align: (left, left, left, left, right, right, right),
  table.hline(stroke: 0.6pt),
  table.header([Date], [Collaborateur], [Tâche], [Description], [Durée], [Tarif], [Montant]),
  table.hline(stroke: 0.4pt),
  ..d.lines.map(l => (
    day(l.date), l.user, l.task, l.folder + if l.description != "" [ — #l.description],
    hm(l.minutes), money(l.rate), money(l.amount),
  )).flatten(),
  table.hline(stroke: 0.6pt),
  table.cell(colspan: 4)[*Total*], [*#hm(d.totals.minutes)*], [], [*#money(d.totals.services)*],
)

#if d.expenses.len() > 0 [
  #v(6mm)
  #text(size: 12pt, weight: "bold")[Frais]
  #v(2mm)
  #table(
    columns: (17mm, 40mm, 1fr, 20mm, 20mm),
    align: (left, left, left, right, right),
    table.hline(stroke: 0.6pt),
    table.header([Date], [Collaborateur], [Type], [Quantité], [Montant]),
    table.hline(stroke: 0.4pt),
    ..d.expenses.map(e => (
      day(e.date), e.user, e.type, if e.quantity != none { str(e.quantity) } else { "" }, money(e.amount),
    )).flatten(),
    table.hline(stroke: 0.6pt),
    table.cell(colspan: 4)[*Total*], [*#money(d.totals.expenses)*],
  )
]
