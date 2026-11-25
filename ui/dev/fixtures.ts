// Dev-only fixtures: a meta for the SLICE domain (SPEC §15–§19) and generated rows.
import type { Col, Command, Meta, Row, Screen, Table, TableCol, View } from "../src/api/contract.ts";

export const enums: Record<string, string[]> = {
  user_kind: ["human", "agent", "service"],
  lang: ["fr", "de", "en"],
  task_kind: ["service", "absence", "vacation"],
  task_billing: ["always", "sometimes", "never"],
  expense_mode: ["both", "user_only", "billing_only"],
  invoice_state: ["draft", "invoiced"],
  export_state: ["none", "created", "exported", "failed"],
  run_state: ["queued", "running", "done", "cancelled", "failed"],
  fn: ["activity", "expense", "invoice", "invoice_state", "period_lock", "validation", "cost", "master_data",
    "scope_direct", "scope_tree", "scope_all", "accounting_sync", "ocr"],
};

const refs: Record<string, string> = {
  manager_id: "user", validated_by: "user", invoiced_by: "user", lead_user_id: "user", parent_id: "folder",
  receipt_file_id: "file", pdf_file_id: "file", principal_id: "user", for_id: "user",
};
const money = /^(rate|amount|hourly_cost|unit_price|billing_unit_price|frozen_rate|frozen_amount|ocr_amount|.*_amount|services|expenses_user|expenses_billed|realized|invoiced|balance_amount|cost|margin.*)$/;
const dates = new Set(["date", "valid_from", "date_from", "date_to"]);
const bools = /^(archived|admin|entry_closed|by_quantity|requires_validation|counts_hours|cancel_requested|ok|to_validate|not_.*|is_.*|entry_open|task_allowed|has_activities|holiday|locked)$/;

// "name" or "name:type" (type = Postgres type or enum name); nullable unless "!" suffix.
export function col(spec: string): Col {
  const req = spec.endsWith("!");
  const [name, t] = spec.replace("!", "").split(":");
  const c: Col = { name, type: "text", format: "", nullable: !req };
  if (t && enums[t]) Object.assign(c, { type: t, enum: t });
  else if (t) c.type = t;
  else if (name === "id") Object.assign(c, { type: "int8", nullable: false });
  else if (refs[name]) Object.assign(c, { type: "int8", ref: refs[name] });
  else if (name.endsWith("_id") && !/^(external|principal|for|owner|root|row)_id$/.test(name)) Object.assign(c, { type: "int8", ref: name.slice(0, -3) });
  else if (/_id$|^row_id$/.test(name)) c.type = "int8";
  else if (name === "span_tod") Object.assign(c, { type: "int4range", format: "tod" });
  else if (name === "week_min") Object.assign(c, { type: "_int4", format: "min" });
  else if (name.endsWith("_min")) Object.assign(c, { type: "int4", format: "min" });
  else if (name.endsWith("_tod")) Object.assign(c, { type: "int4", format: "tod" });
  else if (name.endsWith("_pct")) Object.assign(c, { type: "numeric", format: "pct" });
  else if (name.endsWith("_coef")) Object.assign(c, { type: "numeric", format: "coef" });
  else if (name.endsWith("_at")) c.type = "timestamptz";
  else if (dates.has(name)) c.type = "date";
  else if (money.test(name)) Object.assign(c, { type: "numeric", format: "money" });
  else if (bools.test(name)) c.type = "bool";
  else if (/^(quantity|vacation_days|day_type|total|done|count|status|activity_count)$/.test(name)) c.type = name === "quantity" ? "numeric" : "int4";
  return c;
}
const cols = (s: string) => s.split(/\s+/).filter(Boolean).map(col);

type TOpts = { write?: string; read?: string; child?: [string, string]; under?: string; anchor?: [string, string];
  guarded?: string; label?: string; conditions?: string; actions?: Record<string, string>; extra?: string; crud?: boolean };

export const tables: Table[] = [];
export const projected: Record<string, Col[]> = {}; // projection columns beyond the table's

function T(name: string, columns: string, o: TOpts = {}) {
  const guarded = new Set((o.guarded ?? "").split(" "));
  const tc: TableCol[] = cols(`id ${columns}`).map((c) => ({ ...c, guarded: guarded.has(c.name), has_default: c.name === "id" || c.name === "archived" || c.type === "bool" }));
  const conditions = (o.conditions ?? "").split(" ").filter(Boolean);
  const actions = Object.fromEntries(Object.entries(o.actions ?? {}).map(([k, v]) => [k, v.split(" ")]));
  if (!o.child) {
    actions.editable ??= [];
    actions.deletable ??= [];
  }
  tables.push({ name, root: o.child?.[0] ?? name, child_fk: o.child?.[1], under: o.under, write: o.write ?? "master_data", read: o.read ?? "",
    anchor: o.anchor && { column: o.anchor[0], reach: o.anchor[1] }, label: o.label ?? "name", crud: o.crud ?? true,
    columns: tc, conditions, actions });
  projected[name] = cols(`${o.extra ?? ""} ${o.conditions ?? ""}`);
}

T("company", "name! address vat_no currency!");
T("user", "kind:user_kind! subject! name! manager_id lang:lang! archived");
T("user_group", "name! admin");
T("user_group_member", "user_group_id! user_id!", { child: ["user_group", "user_group_id"], label: "user_id" });
T("user_group_fn", "user_group_id! fn:fn!", { child: ["user_group", "user_group_id"], label: "fn" });
T("account", "external_id name! address archived");
T("project", "external_id account_id! name! rate entry_closed lead_user_id archived");
T("folder", "project_id! parent_id name! rate estimated_min entry_closed");
T("folder_task", "folder_id! task_id!", { child: ["folder", "folder_id"], label: "task_id" });
T("task", "name! kind:task_kind! billing:task_billing! requires_validation counts_hours rate archived");
T("user_task_rate", "user_id! task_id! rate!", { label: "rate" });
T("work_period", "user_id! valid_from! week_min! vacation_days! activity_pct!", { anchor: ["user_id", "user"], label: "valid_from" });
T("cost_rate", "user_id! valid_from! hourly_cost!", { read: "cost", write: "cost", label: "valid_from" });
T("public_holiday", "date! label! deducted_min", { label: "label" });
T("hour_class", "label! hours_coef! rate_coef! requires_validation", { label: "label" });
T("hour_class_slot", "hour_class_id! day_type! span_tod!", { child: ["hour_class", "hour_class_id"], label: "span_tod" });
T("activity", "user_id! date! start_tod! duration_min! billed_min folder_id! task_id! description validated_by validated_at invoice_id frozen_rate frozen_billed_min frozen_amount", {
  write: "activity", under: "company", anchor: ["user_id", "user"], label: "description",
  guarded: "validated_by validated_at invoice_id frozen_rate frozen_billed_min frozen_amount",
  extra: "project_id kind:task_kind rate amount counted_min", conditions: "to_validate not_locked not_invoiced not_validated is_validated entry_open task_allowed",
  actions: { editable: "not_locked not_invoiced not_validated entry_open task_allowed", deletable: "not_locked not_invoiced not_validated",
    validatable: "to_validate not_validated", unvalidatable: "is_validated not_invoiced" } });
T("expense_type", "name! by_quantity unit_price billing_unit_price mode:expense_mode! archived");
T("expense", "activity_id! expense_type_id! quantity amount mode:expense_mode receipt_file_id ocr_amount frozen_user_amount frozen_billed_amount", {
  write: "expense", child: ["activity", "activity_id"], label: "amount", guarded: "receipt_file_id ocr_amount frozen_user_amount frozen_billed_amount",
  extra: "user_amount billed_amount" });
T("period_lock", "company_id! date_from! date_to! user_id", { write: "period_lock", child: ["company", "company_id"], label: "date_from" });
T("invoice", "project_id! date_from! date_to! invoiced_at invoiced_by pdf_file_id", {
  write: "invoice", label: "id", guarded: "invoiced_at invoiced_by pdf_file_id",
  extra: "account_id state:invoice_state activity_count services_amount expenses_amount total_amount export_state:export_state",
  conditions: "is_draft is_invoiced has_activities not_exported",
  actions: { editable: "is_draft", deletable: "is_draft", invoiceable: "is_draft has_activities", billed: "is_invoiced has_activities",
    revertible: "is_invoiced not_exported", exportable: "is_invoiced not_exported" } });
T("connection", "kind! name! endpoint secret_name cursor pulled_at pull_error", { crud: false });
T("delivery", "connection_id! kind root_table root_id payload:jsonb created_at", { crud: false, write: "invoice", label: "id", extra: "attempts:int4 last_detail" });
T("run", "name! input:jsonb state:run_state! total done rejected:jsonb error cancel_requested created_at finished_at", { crud: false, write: "", label: "name" });
T("saved_view", "owner_id! screen! name! state:jsonb!", { write: "", anchor: ["owner_id", "self"] });

const tableOf = (n: string) => tables.find((t) => t.name === n)!;
const listCols = (n: string) => [...tableOf(n).columns.map(({ guarded: _g, has_default: _h, ...c }) => c), ...projected[n]];

export const views: View[] = [];
function V(name: string, params: string, columns: Col[] | string, roots: string, needs = "") {
  views.push({ name, params: cols(params).map((c) => ({ ...c, nullable: false })), columns: typeof columns === "string" ? cols(columns) : columns, roots: roots.split(" "), needs });
}
for (const t of tables) {
  V(t.name, "", listCols(t.name), t.root, t.read);
  const extra = { folder: " project_id", task: " billing:task_billing" }[t.name] ?? "";
  V(`${t.name}.lookup`, "", `id label${extra}`, t.name === "folder" ? "folder project" : t.root, t.read);
}
V("folder_task_allowed", "folder_id", "task_id", "folder");
const actCols = "id date user_id project_id folder_id task_id start_tod duration_min billed_min counted_min rate amount description validated_by invoice_id to_validate not_locked not_invoiced not_validated is_validated entry_open task_allowed";
V("activity_grid", "date_from date_to", actCols, "activity folder project task user");
V("activity_to_validate", "date_from date_to", actCols, "activity folder project task user", "validation");
V("timesheet", "user_id date_from date_to", actCols, "activity folder project task");
V("timesheet_day", "user_id date_from date_to", "date due_min done_min holiday:text locked", "activity work_period public_holiday company");
V("expense_by_activity", "activity_id", "id expense_type_id quantity amount mode:expense_mode user_amount billed_amount receipt_file_id ocr_amount", "activity expense_type");
V("activity_class", "activity_id", "hour_class_id class_min amount", "activity hour_class");
V("balance", "date_from date_to", "user_id carried_min due_min done_min balance_min vac_carried_min vac_available_min vac_taken_min vac_balance_min", "activity work_period public_holiday user");
const profit = "project_id folder_id duration_min billed_min services expenses_billed expenses_user realized invoiced balance_amount";
V("profitability_cost", "date_from date_to", `${profit} cost margin_services margin_balance`, "activity project folder invoice cost_rate", "cost");
V("profitability", "date_from date_to", profit, "activity project folder invoice", "invoice");
V("invoice_activity", "invoice_id", actCols, "invoice activity", "invoice");
V("accounting_export", "invoice_id", "id created_at export_state:export_state attempts:int4 last_detail", "delivery", "invoice");
V("history", "root_table root_id:int8", "at command principal_id for_id tbl row_id:int8 changes:jsonb", "");

export const commands: Command[] = [];
function C(name: string, kind: Command["kind"], o: Partial<Command> & { inputs?: string } = {}) {
  const { inputs, ...rest } = o;
  commands.push({ name, table: name.split(".")[0], kind, rootless: false, input: cols(inputs ?? ""), needs: "", ...rest });
}
for (const t of tables.filter((t) => t.crud)) {
  const input = t.columns.filter((c) => !c.guarded && c.name !== "id").map(({ guarded: _g, has_default: _h, ...c }) => c);
  // SPEC §7: a child table's CRUD is When + Ensure editable of its root.
  const child = t.root !== t.name;
  commands.push({ name: `${t.name}.create`, table: t.name, kind: "create", rootless: true, input, when: child ? "editable" : undefined, ensure: "editable", needs: t.write });
  commands.push({ name: `${t.name}.update`, table: t.name, kind: "update", rootless: false, input: input.map((c) => ({ ...c, nullable: true })), when: "editable", ensure: "editable", needs: t.write });
  commands.push({ name: `${t.name}.delete`, table: t.name, kind: "delete", rootless: false, input: [], when: child ? "editable" : "deletable", ensure: child ? "editable" : undefined, needs: t.write });
}
for (const t of ["saved_view"]) {
  const input = cols("owner_id screen name state:jsonb");
  commands.push({ name: `${t}.create`, table: t, kind: "create", rootless: true, input, needs: "" });
  commands.push({ name: `${t}.update`, table: t, kind: "update", rootless: false, input: cols("name state:jsonb"), needs: "" });
  commands.push({ name: `${t}.delete`, table: t, kind: "delete", rootless: false, input: [], needs: "" });
}
C("activity.validate", "custom", { when: "validatable", needs: "validation", reach: "team" });
C("activity.unvalidate", "custom", { when: "unvalidatable", needs: "validation", reach: "team" });
C("expense.attach_receipt", "custom", { when: "editable", ensure: "editable", needs: "expense", inputs: "receipt_file_id!" });
C("invoice.mark_invoiced", "custom", { when: "invoiceable", ensure: "billed", needs: "invoice_state" });
C("invoice.back_to_draft", "custom", { when: "revertible", needs: "invoice_state" });
C("invoice.print", "custom", { needs: "invoice" });
C("invoice.export", "custom", { when: "exportable", needs: "invoice" });
C("invoice.run", "run", { needs: "invoice", inputs: "date_from! date_to!" });
C("delivery.retry", "custom", { needs: "invoice" });
C("connection.pull", "custom", { needs: "master_data" });
C("run.cancel", "custom");

const S = (id: string, group: string, views: string[], o: Partial<Screen> = {}): Screen =>
  ({ id, group, views, defaults: {}, commands: [], children: [], ...o });
const expenses = S("activity.expenses", "time", ["expense_by_activity"], { table: "expense", commands: ["expense.attach_receipt"], link: { activity_id: "id" } });
const classes = S("activity.classes", "time", ["activity_class"], { link: { activity_id: "id" } });
const sums = (s: string) => s.split(" ").map((c) => ({ col: c, fn: "sum" as const }));
const year = { date_from: "$year_start", date_to: "$today" };
const masterChildren: Record<string, Screen[]> = {
  user_group: [S("user_group.members", "master_data", ["user_group_member"], { table: "user_group_member", link: { user_group_id: "id" } }),
    S("user_group.fns", "master_data", ["user_group_fn"], { table: "user_group_fn", link: { user_group_id: "id" } })],
  hour_class: [S("hour_class.slots", "master_data", ["hour_class_slot"], { table: "hour_class_slot", link: { hour_class_id: "id" } })],
};
export const screens: Screen[] = [
  S("entry", "time", ["timesheet", "timesheet_day"], { component: "entry", table: "activity", defaults: { user_id: "$self", date_from: "$days(-6)", date_to: "$today" }, children: [expenses, classes] }),
  S("activities", "time", ["activity_grid"], { table: "activity", commands: ["activity.validate", "activity.unvalidate"], children: [expenses, classes],
    defaults: { date_from: "$days(-365)", date_to: "$today" }, wrap: { sort: [{ col: "date", dir: "desc" }], agg: sums("duration_min billed_min counted_min amount") } }),
  S("validation", "time", ["activity_to_validate"], { commands: ["activity.validate"], defaults: { date_from: "$days(-90)", date_to: "$today" }, wrap: { agg: sums("duration_min amount") } }),
  S("locks", "time", ["period_lock"], { table: "period_lock" }),
  S("invoicing", "billing", ["invoice"], { table: "invoice", defaults: { date_from: "$month_start", date_to: "$today" },
    commands: ["invoice.run", "invoice.mark_invoiced", "invoice.back_to_draft", "invoice.print", "invoice.export"], wrap: { sort: [{ col: "date_to", dir: "desc" }], agg: sums("services_amount expenses_amount total_amount") },
    children: [S("invoice.activities", "billing", ["invoice_activity"], { link: { invoice_id: "id" }, wrap: { agg: sums("duration_min amount") } }),
      S("invoice.exports", "billing", ["accounting_export"], { commands: ["delivery.retry"], link: { invoice_id: "id" } })] }),
  S("balances", "analysis", ["balance"], { defaults: year }),
  S("profitability", "analysis", ["profitability_cost", "profitability"], { defaults: year, wrap: { group: ["project_id"], agg: sums("duration_min billed_min services expenses_billed expenses_user realized invoiced balance_amount cost margin_services margin_balance") } }),
  ...["account", "project", "folder", "task", "expense_type", "user", "user_group", "work_period", "cost_rate", "public_holiday", "hour_class", "user_task_rate", "company"]
    .map((t) => S(t, "master_data", [t], { table: t, children: masterChildren[t] ?? [] })),
  S("accounting", "system", ["connection"], { table: "connection", commands: ["connection.pull"],
    children: [S("connection.deliveries", "system", ["delivery"], { link: { connection_id: "id" }, commands: ["delivery.retry"] })] }),
];

export const meta: Meta = { tables, views, commands, screens, enums, functions: enums.fn, groups: ["time", "billing", "analysis", "master_data", "system"] };

// ---- data ----
let seed = 42;
const rnd = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32);
const pick = <T>(a: T[]) => a[Math.floor(rnd() * a.length)];
const iso = (d: Date) => d.toISOString().slice(0, 10);
const today = new Date();
const daysAgo = (n: number) => iso(new Date(Date.UTC(today.getFullYear(), today.getMonth(), today.getDate() - n)));

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- fixture rows are loosely typed
export const db: Record<string, Record<string, any>[]> =Object.fromEntries(tables.map((t) => [t.name, []]));
const first = "Alice Bruno Chloé David Emma Félix Gina Hugo Inès Jonas Karin Luca Mia Noah Olga Paul Rita Sven Tina Yann".split(" ");
const last = "Martin Keller Rossi Weber Favre Meier Bernard Huber Rochat Schmid".split(" ");
db.company.push({ id: 1, name: "Demo Services SA", address: "Rue du Lac 1, 1000 Ville", vat_no: "CHE-000.000.000", currency: "CHF" });
first.forEach((f, i) => db.user.push({ id: i + 1, kind: "human", subject: i ? `user${i}` : "admin", name: `${f} ${last[i % last.length]}`, manager_id: i ? 1 + Math.floor(i / 5) : null, lang: "fr", archived: false }));
["admin", "manager", "finance", "collaborateur"].forEach((name, i) => db.user_group.push({ id: i + 1, name, admin: i === 0 }));
db.user.forEach((u) => db.user_group_member.push({ id: u.id, user_group_id: u.id === 1 ? 1 : 4, user_id: u.id }));
enums.fn.forEach((fn, i) => db.user_group_fn.push({ id: i + 1, user_group_id: 1, fn }));
const syl = "nor vik sol alp ber cas dor eli fal gra hel lum mar nov ori pal ric sta ter val".split(" ");
const word = () => { const w = pick(syl) + pick(syl); return w[0].toUpperCase() + w.slice(1); };
for (let i = 1; i <= 30; i++) db.account.push({ id: i, external_id: `A${String(i).padStart(4, "0")}`, name: `${word()} ${pick(["SA", "Sàrl", "AG", "GmbH"])}`, address: null, archived: false });
for (let i = 1; i <= 150; i++) db.project.push({ id: i, external_id: `P${String(i).padStart(5, "0")}`, account_id: 1 + (i % 30), name: `${pick(["Refonte", "Migration", "Audit", "Portail", "Support", "Étude"])} ${word()}`, rate: pick([null, 140, 160, 180]), entry_closed: i % 37 === 0, lead_user_id: 1 + (i % 20), archived: false });
for (let i = 1; i <= 600; i++) db.folder.push({ id: i, project_id: 1 + (i % 150), parent_id: null, name: pick(["Analyse", "Réalisation", "Tests", "Déploiement", "Gestion"]) + ` ${1 + (i % 4)}`, rate: i % 5 ? null : 150, estimated_min: i % 5 === 1 ? 6000 : null, entry_closed: false });
const tasks: [string, string, string, boolean][] = [["Développement", "service", "always", false], ["Analyse", "service", "always", false], ["Conseil", "service", "sometimes", false], ["Gestion de projet", "service", "sometimes", false], ["Support", "service", "always", false], ["Formation interne", "service", "never", false], ["Déplacement", "service", "never", true], ["Maladie", "absence", "never", false], ["Vacances", "vacation", "never", false], ["Heures de nuit", "service", "always", true]];
tasks.forEach(([name, kind, billing, rv], i) => db.task.push({ id: i + 1, name, kind, billing, requires_validation: rv, counts_hours: kind !== "absence", rate: 120 + 10 * (i % 4), archived: false }));
db.expense_type.push({ id: 1, name: "Kilomètres", by_quantity: true, unit_price: 0.7, billing_unit_price: 0.9, mode: "both", archived: false },
  { id: 2, name: "Repas", by_quantity: false, unit_price: null, billing_unit_price: null, mode: "user_only", archived: false },
  { id: 3, name: "Hôtel", by_quantity: false, unit_price: null, billing_unit_price: null, mode: "both", archived: false });
db.user.forEach((u) => {
  db.work_period.push({ id: u.id, user_id: u.id, valid_from: daysAgo(900), week_min: [504, 504, 504, 504, 504, 0, 0], vacation_days: 25, activity_pct: u.id % 4 ? 100 : 80 });
  db.cost_rate.push({ id: u.id, user_id: u.id, valid_from: daysAgo(900), hourly_cost: 80 + (u.id % 5) * 10 });
});
["Nouvel an", "Vendredi saint", "Lundi de Pâques", "Ascension", "Fête nationale", "Noël"].forEach((label, i) => db.public_holiday.push({ id: i + 1, date: daysAgo(30 + i * 55), label, deducted_min: null }));
[["Nuit", 1.25, 1.5, true], ["Samedi", 1.25, 1.25, false], ["Dimanche et férié", 1.5, 2, true]].forEach(([label, hc, rc, rv], i) => db.hour_class.push({ id: i + 1, label, hours_coef: hc, rate_coef: rc, requires_validation: rv }));
db.hour_class_slot.push({ id: 1, hour_class_id: 1, day_type: 1, span_tod: "[0,360)" }, { id: 2, hour_class_id: 1, day_type: 1, span_tod: "[1320,1440)" }, { id: 3, hour_class_id: 2, day_type: 6, span_tod: "[0,1440)" }, { id: 4, hour_class_id: 3, day_type: 7, span_tod: "[0,1440)" });
// Monday and Tuesday of last week are locked for user 1.
const dow = (today.getDay() + 6) % 7;
db.period_lock.push({ id: 1, company_id: 1, date_from: daysAgo(400), date_to: daysAgo(200), user_id: null },
  { id: 2, company_id: 1, date_from: daysAgo(dow + 7), date_to: daysAgo(dow + 6), user_id: 1 });
// Each folder allows 8 of the 10 tasks.
const allowed = (f: number, t: number) => (f * 3 + t) % 5 !== 0;
for (const f of db.folder) for (const t of db.task) if (allowed(f.id, t.id)) db.folder_task.push({ id: db.folder_task.length + 1, folder_id: f.id, task_id: t.id });
const descr = ["Atelier client", "Revue de code", "Correctifs", "Spécifications", "Réunion de suivi", "Tests d'intégration", "Mise en production", "Documentation", ""];
const act = (user_id: number, day: number, start_tod: number, duration_min: number, folder_id: number, task: number, validated: boolean) => {
  const task_id = allowed(folder_id, task) ? task : task === 1 ? 2 : 1;
  db.activity.push({ id: db.activity.length + 1, user_id, date: daysAgo(day), start_tod, duration_min, billed_min: null, folder_id, task_id, description: pick(descr),
    validated_by: validated ? 1 : null, validated_at: validated ? `${daysAgo(day - 1)}T17:00:00Z` : null, invoice_id: null, frozen_rate: null, frozen_billed_min: null, frozen_amount: null });
};
// User 1 (the dev session) has regular working days on a few folders; today only the morning.
const holidays = new Set(db.public_holiday.map((h) => h.date));
for (let day = 364; day >= 0; day--) {
  const wd = new Date(daysAgo(day)).getUTCDay();
  if (!wd || wd === 6 || holidays.has(daysAgo(day))) continue;
  for (const [from, to] of day ? [[480, 720], [780, 1050]] : [[480, 720]]) {
    for (let t = from; t < to; ) {
      const d = Math.min(to - t, 30 * (2 + Math.floor(rnd() * 5)));
      act(1, day, t, d, pick([12, 47, 88, 131, 205, 302]), 1 + Math.floor(rnd() * 5), day > 21);
      t += d;
    }
  }
}
while (db.activity.length < 50000) {
  const day = Math.floor(rnd() * 365);
  const task = rnd() < 0.9 ? 1 + Math.floor(rnd() * 6) : 7 + Math.floor(rnd() * 4);
  act(2 + Math.floor(rnd() * 19), day, 480 + 30 * Math.floor(rnd() * 16), 15 * (1 + Math.floor(rnd() * 16)), 1 + Math.floor(rnd() * 600), task, day > 60 || rnd() < 0.3);
}
for (let i = 1; i <= 80; i++) db.invoice.push({ id: i, project_id: 1 + (i % 150), date_from: daysAgo(30 * (1 + (i % 10)) + 30), date_to: daysAgo(30 * (1 + (i % 10))), invoiced_at: i % 3 ? `${daysAgo(30 * (1 + (i % 10)) - 2)}T10:00:00Z` : null, invoiced_by: i % 3 ? 1 : null, pdf_file_id: null });
for (const a of db.activity) {
  const inv = db.invoice.find((v) => v.invoiced_at && v.project_id === db.folder[(a.folder_id as number) - 1].project_id && a.date >= v.date_from && a.date <= v.date_to);
  if (inv) a.invoice_id = inv.id;
}
for (let i = 1; i <= 2000; i++) db.expense.push({ id: i, activity_id: 1 + Math.floor(rnd() * 50000), expense_type_id: 1 + (i % 3), quantity: i % 3 === 1 ? 20 + (i % 80) : null, amount: i % 3 === 1 ? null : 15 + (i % 90), mode: null, receipt_file_id: null, ocr_amount: null, frozen_user_amount: null, frozen_billed_amount: null });
db.connection.push({ id: 1, kind: "accounting", name: "Comptabilité", endpoint: "http://localhost:8070", secret_name: "ACCT", cursor: "0", pulled_at: `${daysAgo(0)}T06:00:00Z`, pull_error: null });
db.invoice.filter((v) => v.invoiced_at && (v.id as number) % 2).forEach((v, i) => db.delivery.push({ id: i + 1, connection_id: 1, kind: "invoice", root_table: "invoice", root_id: v.id, payload: {}, created_at: v.invoiced_at, attempts: 1 + (i % 3), last_detail: i % 5 ? "200 OK" : "503 Service Unavailable" }));

export const labels: Record<string, string> = {
  "g.time": "Temps", "g.billing": "Facturation", "g.analysis": "Analyse", "g.master_data": "Données de base", "g.system": "Système",
  "s.entry": "Saisie", "s.activities": "Activités", "s.validation": "À valider", "s.locks": "Périodes verrouillées", "s.invoicing": "Facturation",
  "s.balances": "Soldes", "s.profitability": "Rentabilité", "s.accounting": "Comptabilité", "s.activity.expenses": "Frais", "s.activity.classes": "Classes d'heures",
  "s.invoice.activities": "Activités", "s.invoice.exports": "Exports", "s.connection.deliveries": "Envois", "s.user_group.members": "Membres", "s.user_group.fns": "Fonctions", "s.hour_class.slots": "Créneaux",
  "s.account": "Clients", "s.project": "Projets", "s.folder": "Dossiers", "s.task": "Tâches", "s.expense_type": "Types de frais", "s.user": "Utilisateurs", "s.user_group": "Groupes",
  "s.work_period": "Périodes de travail", "s.cost_rate": "Prix de revient", "s.public_holiday": "Jours fériés", "s.hour_class": "Classes d'heures", "s.user_task_rate": "Tarifs par utilisateur", "s.company": "Société",
  "t.activity": "Activité", "t.invoice": "Facture", "t.expense": "Frais", "t.period_lock": "Verrou", "t.account": "Client", "t.project": "Projet", "t.folder": "Dossier", "t.task": "Tâche", "t.user": "Utilisateur",
  "t.user_group": "Groupe", "t.work_period": "Période de travail", "t.cost_rate": "Prix de revient", "t.public_holiday": "Jour férié", "t.hour_class": "Classe d'heures", "t.hour_class_slot": "Créneau",
  "t.expense_type": "Type de frais", "t.user_task_rate": "Tarif", "t.company": "Société", "t.connection": "Connexion", "t.user_group_member": "Membre", "t.user_group_fn": "Fonction", "t.saved_view": "Vue",
  "c.id": "N°", "c.name": "Nom", "c.label": "Libellé", "c.date": "Date", "c.user_id": "Collaborateur", "c.project_id": "Projet", "c.folder_id": "Dossier", "c.task_id": "Tâche", "c.account_id": "Client",
  "c.start_tod": "Début", "c.duration_min": "Durée", "c.billed_min": "Facturé", "c.counted_min": "Compté", "c.amount": "Montant", "c.rate": "Tarif", "c.description": "Description",
  "c.validated_by": "Validé par", "c.validated_at": "Validé le", "c.invoice_id": "Facture", "c.kind": "Genre", "c.billing": "Facturation", "c.requires_validation": "Validation requise",
  "c.counts_hours": "Compte les heures", "c.archived": "Archivé", "c.external_id": "Réf. externe", "c.address": "Adresse", "c.entry_closed": "Saisie fermée", "c.lead_user_id": "Responsable",
  "c.parent_id": "Dossier parent", "c.estimated_min": "Estimé", "c.date_from": "Du", "c.date_to": "Au", "c.invoiced_at": "Facturée le", "c.invoiced_by": "Facturée par", "c.pdf_file_id": "PDF",
  "c.state": "État", "c.activity_count": "Activités", "c.services_amount": "Prestations", "c.expenses_amount": "Frais", "c.total_amount": "Total", "c.export_state": "Export",
  "c.manager_id": "Responsable", "c.lang": "Langue", "c.subject": "Sujet", "c.valid_from": "Dès le", "c.week_min": "Heures par jour", "c.vacation_days": "Jours de vacances",
  "c.activity_pct": "Taux d'activité", "c.hourly_cost": "Coût horaire", "c.deducted_min": "Heures déduites", "c.hours_coef": "Coef. heures", "c.rate_coef": "Coef. tarif",
  "c.hour_class_id": "Classe", "c.day_type": "Type de jour", "c.span_tod": "Créneau", "c.expense_type_id": "Type", "c.quantity": "Quantité", "c.mode": "Mode", "c.receipt_file_id": "Justificatif",
  "c.ocr_amount": "Montant OCR", "c.user_amount": "Montant collaborateur", "c.billed_amount": "Montant facturé", "c.unit_price": "Prix unitaire", "c.billing_unit_price": "Prix facturé",
  "c.by_quantity": "Par quantité", "c.company_id": "Société", "c.admin": "Admin", "c.fn": "Fonction", "c.user_group_id": "Groupe", "c.endpoint": "Adresse", "c.pulled_at": "Dernier import",
  "c.pull_error": "Erreur", "c.cursor": "Curseur", "c.secret_name": "Secret", "c.currency": "Monnaie", "c.vat_no": "N° TVA", "c.receipt": "Justificatif",
  "c.carried_min": "Report", "c.due_min": "À faire", "c.done_min": "Fait", "c.balance_min": "Solde", "c.vac_carried_min": "Vac. report", "c.vac_available_min": "Vac. dispo",
  "c.vac_taken_min": "Vac. prises", "c.vac_balance_min": "Vac. solde", "c.services": "Prestations", "c.expenses_billed": "Frais facturés", "c.expenses_user": "Frais collab.",
  "c.realized": "Réalisé", "c.invoiced": "Montant facturé", "c.balance_amount": "Solde", "c.cost": "Prix de revient", "c.margin_services": "Marge prest.", "c.margin_balance": "Marge solde",
  "c.class_min": "Durée", "c.created_at": "Créé le", "c.attempts": "Tentatives", "c.last_detail": "Dernier détail", "c.connection_id": "Connexion", "c.root_id": "Objet", "c.holiday": "Férié", "c.locked": "Verrouillé",
  "c.to_validate": "À valider", "c.not_locked": "Non verrouillée", "c.not_invoiced": "Non facturée", "c.not_validated": "Non validée", "c.is_validated": "Validée", "c.entry_open": "Saisie ouverte", "c.task_allowed": "Tâche permise",
  "cmd.activity.validate": "Valider", "cmd.activity.unvalidate": "Dévalider", "cmd.invoice.run": "Run de facturation", "cmd.invoice.mark_invoiced": "Facturer", "cmd.invoice.back_to_draft": "Remettre en cours",
  "cmd.invoice.print": "Imprimer", "cmd.invoice.export": "Exporter", "cmd.delivery.retry": "Relancer", "cmd.connection.pull": "Importer", "cmd.run.cancel": "Annuler le run", "cmd.expense.attach_receipt": "Joindre le justificatif",
  "cond.to_validate": "À valider", "cond.not_locked": "Période non verrouillée", "cond.not_invoiced": "Pas encore facturée", "cond.not_validated": "Pas encore validée", "cond.is_validated": "Validée",
  "cond.entry_open": "Dossier et projet ouverts à la saisie", "cond.task_allowed": "Tâche permise dans le dossier", "cond.is_draft": "Facture en cours", "cond.is_invoiced": "Facture facturée",
  "cond.has_activities": "A des activités", "cond.not_exported": "Pas encore exportée",
  "constraint.public_holiday_date_key": "Un jour férié existe déjà à cette date", "constraint.saved_view_owner_id_screen_name_key": "Une vue porte déjà ce nom",
  "e.run_state.queued": "En attente", "e.run_state.running": "En cours", "e.run_state.done": "Terminé", "e.run_state.cancelled": "Annulé", "e.run_state.failed": "Échoué",
  "e.invoice_state.draft": "En cours", "e.invoice_state.invoiced": "Facturée", "e.export_state.none": "—", "e.export_state.created": "Créé", "e.export_state.exported": "Exporté", "e.export_state.failed": "Échec",
  "e.task_kind.service": "Prestation", "e.task_kind.absence": "Absence", "e.task_kind.vacation": "Vacances", "e.task_billing.always": "Toujours", "e.task_billing.sometimes": "Parfois", "e.task_billing.never": "Jamais",
  "e.expense_mode.both": "Rembourser et facturer", "e.expense_mode.user_only": "Collaborateur seul", "e.expense_mode.billing_only": "Facturation seule",
  "e.user_kind.human": "Humain", "e.user_kind.agent": "Agent", "e.user_kind.service": "Service", "e.lang.fr": "Français", "e.lang.de": "Deutsch", "e.lang.en": "English",
};
for (const c of commands) labels[`cmd.${c.name}`] ??= { create: "Nouveau", update: "Modifier", delete: "Supprimer" }[c.kind as string] ?? c.name;
for (const f of enums.fn) labels[`e.fn.${f}`] = labels[`fn.${f}`] = f.replace(/_/g, " ");

export const translations: Record<string, Record<string, string>> = {
  de: { "g.time": "Zeit", "g.billing": "Fakturierung", "g.analysis": "Analyse", "g.master_data": "Stammdaten", "g.system": "System", "s.activities": "Aktivitäten", "s.entry": "Erfassung",
    "s.validation": "Zu validieren", "s.invoicing": "Fakturierung", "s.balances": "Salden", "s.profitability": "Rentabilität", "s.locks": "Gesperrte Perioden", "c.date": "Datum", "c.user_id": "Mitarbeiter",
    "c.project_id": "Projekt", "c.folder_id": "Ordner", "c.task_id": "Aufgabe", "c.start_tod": "Beginn", "c.duration_min": "Dauer", "c.billed_min": "Verrechnet", "c.counted_min": "Gezählt", "c.amount": "Betrag",
    "c.description": "Beschreibung", "c.validated_by": "Validiert von", "c.invoice_id": "Rechnung", "cmd.activity.validate": "Validieren", "cmd.activity.unvalidate": "Validierung aufheben",
    "cmd.activity.create": "Neu", "cmd.activity.update": "Bearbeiten", "cmd.activity.delete": "Löschen", "cond.to_validate": "Zu validieren", "cond.not_validated": "Noch nicht validiert" },
  en: { "g.time": "Time", "g.billing": "Billing", "g.analysis": "Analysis", "g.master_data": "Master data", "g.system": "System", "s.activities": "Activities", "s.entry": "Time entry",
    "s.validation": "To validate", "s.invoicing": "Invoicing", "s.balances": "Balances", "s.profitability": "Profitability", "s.locks": "Locked periods", "c.date": "Date", "c.user_id": "Employee",
    "c.project_id": "Project", "c.folder_id": "Folder", "c.task_id": "Task", "c.start_tod": "Start", "c.duration_min": "Duration", "c.billed_min": "Billed", "c.counted_min": "Counted", "c.amount": "Amount",
    "c.description": "Description", "c.validated_by": "Validated by", "c.invoice_id": "Invoice", "cmd.activity.validate": "Validate", "cmd.activity.unvalidate": "Unvalidate",
    "cmd.activity.create": "New", "cmd.activity.update": "Edit", "cmd.activity.delete": "Delete", "cond.to_validate": "To validate", "cond.not_validated": "Not yet validated" },
};
