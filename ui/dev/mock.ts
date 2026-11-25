// Dev-only mock of the SPEC §13 HTTP contract, served by the Vite dev server. Never part of the build.
import type { Plugin } from "vite";
import type { IncomingMessage, ServerResponse } from "node:http";
import { createHash, randomUUID } from "node:crypto";
import type { Filter, Row, ViewBody } from "../src/api/contract.ts";

type Fx = typeof import("./fixtures.ts");
type Result = { status: number; body: unknown };

export function mock(): Plugin {
  return {
    name: "sor-mock",
    apply: "serve",
    async configureServer(server) {
      const fx = await import("./fixtures.ts");
      server.middlewares.use((req, res, next) => handle(fx, req, res).then((done) => done || next(), next));
    },
  };
}

const clients = new Set<ServerResponse>();
const blobs = new Map<string, { type: string; data: Buffer }>();
const files = new Map<number, { hash: string; name: string; type: string }>();
const events: Row[] = [];

const problem = (status: number, code: string, extra: object = {}): Result =>
  ({ status, body: { status, title: code, code, ...extra } });

function send(res: ServerResponse, r: Result) {
  res.writeHead(r.status, { "Content-Type": r.status < 300 ? "application/json" : "application/problem+json" });
  res.end(JSON.stringify(r.body));
}

async function read(req: IncomingMessage) {
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  return Buffer.concat(chunks);
}

async function handle(fx: Fx, req: IncomingMessage, res: ServerResponse): Promise<boolean> {
  const url = new URL(req.url!, "http://x");
  const p = url.pathname;
  if (p === "/api/live") {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", Connection: "keep-alive" });
    res.write(": ok\n\n");
    clients.add(res);
    req.on("close", () => clients.delete(res));
    return true;
  }
  if (p.startsWith("/dev-files/")) {
    const hash = p.slice(11);
    if (req.method === "PUT") {
      blobs.set(hash, { type: String(req.headers["content-type"] ?? ""), data: await read(req) });
      res.end();
    } else {
      const b = blobs.get(hash);
      res.writeHead(b ? 200 : 404, { "Content-Type": b?.type ?? "text/plain" });
      res.end(b?.data);
    }
    return true;
  }
  if (!p.startsWith("/api/")) return false;
  await new Promise((r) => setTimeout(r, 5 + Math.random() * 15));
  const body = req.method === "POST" ? JSON.parse((await read(req)).toString() || "{}") : {};
  send(res, route(fx, p, body));
  return true;
}

function route(fx: Fx, p: string, body: Record<string, unknown>): Result {
  const { meta, db, labels, translations } = fx;
  if (p === "/api/me") return { status: 200, body: { id: 1, name: db.user[0].name, kind: "human", lang: "fr", functions: meta.functions, reach: { self: [1], team: db.user.slice(1).map((u) => u.id), user: db.user.map((u) => u.id) } } };
  if (p === "/api/meta") return { status: 200, body: meta };
  if (p.startsWith("/api/labels/")) return { status: 200, body: { ...labels, ...translations[p.slice(12)] } };
  if (p.startsWith("/api/v/")) {
    const name = p.slice(7);
    const v = meta.views.find((x) => x.name === name);
    if (!v) return problem(404, "not_found");
    return { status: 200, body: wrap(viewRows(fx, name, (body.params ?? {}) as Row), body as ViewBody) };
  }
  if (p.startsWith("/api/c/")) return command(fx, p.slice(7), body);
  if (p === "/api/files/presign") return { status: 200, body: blobs.has(body.hash as string) ? { exists: true } : { exists: false, url: `/dev-files/${body.hash}`, headers: {} } };
  if (p === "/api/files") return { status: 200, body: { id: register(body.hash as string, body.name as string, body.type as string) } };
  if (p === "/api/file") {
    const row = viewRows(fx, body.view as string, (body.params ?? {}) as Row).find((r) => r.id === body.id);
    const f = files.get(row?.[body.column as string] as number);
    return f ? { status: 200, body: { url: `/dev-files/${f.hash}`, name: f.name, type: f.type } } : problem(404, "not_found");
  }
  return problem(404, "not_found");
}

function register(hash: string, name: string, type: string) {
  for (const [id, f] of files) if (f.hash === hash) return id;
  const id = files.size + 1;
  files.set(id, { hash, name, type });
  return id;
}

// ---- projections and views ----
const idx = new Map<string, Map<unknown, Row>>();
const get = (fx: Fx, t: string, id: unknown) => {
  let m = idx.get(t);
  if (!m) idx.set(t, (m = new Map(fx.db[t].map((r) => [r.id, r]))));
  return m.get(id);
};
const r2 = (n: number) => Math.round(n * 100) / 100;

function activityP(fx: Fx, a: Row): Row {
  const f = get(fx, "folder", a.folder_id)!;
  const p = get(fx, "project", f.project_id)!;
  const tk = get(fx, "task", a.task_id)!;
  const dur = a.duration_min as number;
  const billed = (a.frozen_billed_min as number) ?? (tk.billing === "always" ? dur : tk.billing === "sometimes" ? ((a.billed_min as number) ?? dur) : 0);
  const rate = (a.frozen_rate as number) ?? (f.rate as number) ?? (p.rate as number) ?? (tk.rate as number) ?? 0;
  const locked = isLocked(fx, a.date as string, a.user_id as number);
  const night = (a.start_tod as number) < 360 || (a.start_tod as number) + dur > 1320;
  return { ...a, project_id: f.project_id, kind: tk.kind, billed_min: billed, rate, amount: (a.frozen_amount as number) ?? r2((rate * billed) / 60),
    counted_min: tk.counts_hours ? (night ? Math.round(dur * 1.25) : dur) : 0,
    to_validate: !!tk.requires_validation || night || (a.id as number) % 7 === 0,
    not_locked: !locked, not_invoiced: a.invoice_id == null, not_validated: a.validated_by == null, is_validated: a.validated_by != null,
    entry_open: !f.entry_closed && !p.entry_closed && !p.archived, task_allowed: allowedTasks(fx, a.folder_id as number).has(a.task_id as number) };
}

const isLocked = (fx: Fx, date: string, user: number) =>
  fx.db.period_lock.some((l) => date >= l.date_from! && date <= l.date_to! && (l.user_id == null || l.user_id === user));

let allowed: Map<number, Set<number>> | null = null;
function allowedTasks(fx: Fx, folder: number) {
  if (!allowed) {
    allowed = new Map();
    for (const r of fx.db.folder_task) {
      if (!allowed.has(r.folder_id)) allowed.set(r.folder_id, new Set());
      allowed.get(r.folder_id)!.add(r.task_id);
    }
  }
  return allowed.get(folder) ?? new Set<number>();
}

// Activities of an invoice: attached once invoiced, else the uninvoiced ones of its project in its period.
let byInvoice: Map<string, Row[]> | null = null;
function invoiceActs(fx: Fx, v: Row) {
  if (!byInvoice) {
    byInvoice = new Map();
    for (const a of fx.db.activity) {
      const k = a.invoice_id != null ? `i${a.invoice_id}` : `p${get(fx, "folder", a.folder_id)!.project_id}`;
      if (!byInvoice.has(k)) byInvoice.set(k, []);
      byInvoice.get(k)!.push(a);
    }
  }
  return (v.invoiced_at ? (byInvoice.get(`i${v.id}`) ?? []) : (byInvoice.get(`p${v.project_id}`) ?? []).filter((a) => inRange(a, v))).map((a) => activityP(fx, a));
}

function invoiceP(fx: Fx, v: Row): Row {
  const acts = invoiceActs(fx, v);
  const services = r2(acts.reduce((s, a) => s + (a.amount as number), 0));
  const d = fx.db.delivery.filter((x) => x.root_id === v.id).at(-1);
  const exp = !d ? "none" : String(d.last_detail).startsWith("200") ? "exported" : "failed";
  return { ...v, account_id: get(fx, "project", v.project_id)?.account_id, state: v.invoiced_at ? "invoiced" : "draft", activity_count: acts.length,
    services_amount: services, expenses_amount: 0, total_amount: services, export_state: exp,
    is_draft: !v.invoiced_at, is_invoiced: !!v.invoiced_at, has_activities: acts.length > 0, not_exported: exp !== "exported" };
}

const project = (fx: Fx, t: string, r: Row) => (t === "activity" ? activityP(fx, r) : t === "invoice" ? invoiceP(fx, r) : r);
const inRange = (a: Row, p: Row) => a.date! >= p.date_from! && a.date! <= p.date_to!;

function viewRows(fx: Fx, name: string, p: Row): Row[] {
  const { db } = fx;
  const acts = () => db.activity.filter((a) => inRange(a, p)).map((a) => activityP(fx, a));
  switch (name) {
    case "activity_grid": return acts();
    case "activity_to_validate": return acts().filter((a) => a.to_validate && a.not_validated && a.user_id !== 1);
    case "timesheet": return acts().filter((a) => a.user_id === p.user_id).sort((x, y) => x.date === y.date ? (x.start_tod as number) - (y.start_tod as number) : x.date! < y.date! ? -1 : 1)
      .map((a) => ({ ...a, billing: get(fx, "task", a.task_id)!.billing }));
    case "folder_task_allowed": return db.task.filter((t) => !t.archived && allowedTasks(fx, p.folder_id as number).has(t.id))
      .map(({ id, name, kind, billing, requires_validation, counts_hours }) => ({ id, name, kind, billing, requires_validation, counts_hours }));
    case "invoice_activity": {
      const v = get(fx, "invoice", p.invoice_id);
      return v ? invoiceActs(fx, v) : [];
    }
    case "timesheet_day": {
      const out: Row[] = [];
      const pct = (db.work_period.find((w) => w.user_id === p.user_id)?.activity_pct ?? 100) / 100;
      const mine = db.activity.filter((a) => a.user_id === p.user_id && inRange(a, p)).map((a) => activityP(fx, a));
      for (let d = new Date(p.date_from as string); d <= new Date(p.date_to as string); d.setUTCDate(d.getUTCDate() + 1)) {
        const date = d.toISOString().slice(0, 10);
        const holiday = db.public_holiday.find((h) => h.date === date);
        const due = d.getUTCDay() % 6 ? 504 - (holiday ? (holiday.deducted_min ?? 504) : 0) : 0;
        const done = mine.filter((a) => a.date === date).reduce((s, a) => s + (a.counted_min as number), 0);
        out.push({ date, due_min: Math.round(due * pct), done_min: done, holiday: holiday?.label ?? null, locked: isLocked(fx, date, p.user_id as number) });
      }
      return out;
    }
    case "expense_by_activity": return db.expense.filter((e) => e.activity_id === p.activity_id).map((e) => ({ ...e, mode: e.mode ?? "both", user_amount: e.amount ?? (e.quantity as number) * 0.7, billed_amount: e.amount ?? (e.quantity as number) * 0.9 }));
    case "activity_class": {
      const a = activityP(fx, get(fx, "activity", p.activity_id)!);
      return [{ hour_class_id: null, class_min: a.duration_min, amount: a.amount }];
    }
    case "balance": {
      const days = Math.round((Math.min(Date.now(), +new Date(p.date_to as string)) - +new Date(p.date_from as string)) / 864e5 * 5 / 7);
      return db.user.map((u) => {
        const mine = db.activity.filter((a) => a.user_id === u.id && inRange(a, p));
        const due = days * 504 * (u.id as number % 4 ? 1 : 0.8);
        const done = mine.reduce((s, a) => s + (a.duration_min as number), 0);
        const carried = ((u.id as number) % 5 - 2) * 150;
        const vac = mine.filter((a) => a.task_id === 9).reduce((s, a) => s + (a.duration_min as number), 0);
        return { user_id: u.id, carried_min: carried, due_min: due, done_min: done, balance_min: carried + done - due, vac_carried_min: 480, vac_available_min: 12600, vac_taken_min: vac, vac_balance_min: 13080 - vac };
      });
    }
    case "profitability":
    case "profitability_cost": {
      const m = new Map<number, Row>();
      for (const a of acts()) {
        const r = m.get(a.folder_id as number) ?? { project_id: a.project_id, folder_id: a.folder_id, duration_min: 0, billed_min: 0, services: 0, expenses_billed: 0, expenses_user: 0, realized: 0, invoiced: 0, balance_amount: 0, cost: 0, margin_services: 0, margin_balance: 0 };
        const n = (k: string, v: number) => (r[k] = r2((r[k] as number) + v));
        n("duration_min", a.duration_min as number); n("billed_min", a.billed_min as number); n("services", a.amount as number); n("realized", a.amount as number);
        if (a.invoice_id) n("invoiced", a.amount as number);
        n("cost", ((a.counted_min as number) / 60) * (80 + ((a.user_id as number) % 5) * 10));
        r.balance_amount = r2((r.realized as number) - (r.invoiced as number));
        r.margin_services = r2((r.services as number) - (r.cost as number));
        r.margin_balance = r2((r.balance_amount as number) - (r.cost as number));
        m.set(a.folder_id as number, r);
      }
      return [...m.values()];
    }
    case "accounting_export": return db.delivery.filter((d) => d.root_id === p.invoice_id).map((d) => ({ ...d, export_state: String(d.last_detail).startsWith("200") ? "exported" : "failed" }));
    case "history": return events.filter((e) => e.root_table === p.root_table && e.root_id === p.root_id);
  }
  if (name.endsWith(".lookup")) {
    const t = name.slice(0, -7);
    const tbl = fx.meta.tables.find((x) => x.name === t)!;
    if (t === "folder") return db.folder.map((r) => {
      const pr = get(fx, "project", r.project_id)!;
      return { id: r.id, label: `${pr.name} / ${r.name}`, project_id: r.project_id, entry_open: !r.entry_closed && !pr.entry_closed && !pr.archived };
    });
    return db[t].map((r) => ({ id: r.id, label: String(r[tbl.label] ?? r.id) }));
  }
  return (db[name] ?? []).map((r) => project(fx, name, r));
}

function test(f: Filter, v: unknown) {
  const x = f.value as never;
  switch (f.op) {
    case "eq": return v === x;
    case "ne": return v !== x;
    case "lt": return (v as never) < x;
    case "le": return (v as never) <= x;
    case "gt": return (v as never) > x;
    case "ge": return (v as never) >= x;
    case "in": return (x as unknown[]).includes(v);
    case "contains": return v != null && String(v).toLowerCase().includes(String(x).toLowerCase());
    case "null": return v == null;
    case "notnull": return v != null;
  }
}

function wrap(rows: Row[], b: ViewBody): Row[] {
  let out = rows.filter((r) => (b.filter ?? []).every((f) => test(f, r[f.col])));
  if (b.group?.length || b.agg?.length) {
    const groups = new Map<string, Row[]>();
    for (const r of out) {
      const k = JSON.stringify((b.group ?? []).map((g) => r[g]));
      if (!groups.has(k)) groups.set(k, []);
      groups.get(k)!.push(r);
    }
    if (!b.group?.length && !groups.size) groups.set("[]", []);
    out = [...groups.entries()].map(([k, rs]) => {
      const row: Row = Object.fromEntries((b.group ?? []).map((g, i) => [g, JSON.parse(k)[i]]));
      for (const a of b.agg ?? []) {
        const vs = rs.map((r) => r[a.col] as number).filter((v) => v != null);
        row[a.fn === "count" ? "count" : `${a.fn}_${a.col}`] =
          a.fn === "count" ? rs.length : a.fn === "sum" ? r2(vs.reduce((s, v) => s + v, 0)) : a.fn === "min" ? Math.min(...vs) : a.fn === "max" ? Math.max(...vs) : r2(vs.reduce((s, v) => s + v, 0) / (vs.length || 1));
      }
      return row;
    });
  }
  for (const s of [...(b.sort ?? [])].reverse()) {
    const d = s.dir === "desc" ? -1 : 1;
    out = [...out].sort((x, y) => (x[s.col] === y[s.col] ? 0 : x[s.col] == null ? 1 : y[s.col] == null ? -1 : (x[s.col]! > y[s.col]! ? d : -d)));
  }
  const off = b.offset ?? 0;
  return out.slice(off, off + Math.min(b.limit ?? 200, 5000));
}

// ---- commands ----
const uniques: Record<string, [string[], string]> = {
  public_holiday: [["date"], "public_holiday_date_key"],
  saved_view: [["owner_id", "screen", "name"], "saved_view_owner_id_screen_name_key"],
};

function command(fx: Fx, name: string, input: Row): Result {
  const { meta, db } = fx;
  const c = meta.commands.find((x) => x.name === name);
  if (!c) return problem(404, "not_found");
  const tbl = meta.tables.find((t) => t.name === c.table)!;
  const rows = db[tbl.name];
  const ids = (input.ids as number[] | undefined) ?? [];
  const targets = ids.map((id) => get(fx, tbl.name, id));
  if (targets.some((r) => !r)) return problem(404, "not_found");
  const rootOf = (r: Row) => (tbl.root === tbl.name ? r : get(fx, tbl.root, r[tbl.child_fk!])!);
  const rootTbl = meta.tables.find((t) => t.name === tbl.root)!;
  const check = (action: string | undefined, rs: Row[]) => {
    const conds = (action && rootTbl.actions[action]) || [];
    return rs.map((r) => project(fx, tbl.root, rootOf(r))).flatMap((r) => {
      const failed = conds.filter((k) => r[k] === false);
      return failed.length ? [{ id: r.id, conditions: failed, row: r }] : [];
    });
  };
  const rejected = check(c.when, targets as Row[]);
  if (rejected.length) return problem(409, "rejected", { action: c.when, rejected });

  const before = new Map(targets.map((r) => [r!.id, { ...r! }]));
  const changed: Row[] = [];
  let run: number | undefined;
  const { ids: _ids, ...fields } = input;
  const set = (r: Row, f: Row) => { Object.assign(r, f); changed.push(r); };
  const now = new Date().toISOString();

  switch (c.kind === "custom" || c.kind === "run" ? name : c.kind) {
    case "create": {
      const id = Math.max(0, ...rows.map((r) => r.id as number)) + 1;
      const r: Row = { ...Object.fromEntries(tbl.columns.map((x) => [x.name, x.type === "bool" ? false : null])), ...fields, id };
      rows.push(r);
      changed.push(r);
      break;
    }
    case "update": targets.forEach((r) => set(r!, fields)); break;
    case "delete": {
      for (const r of targets) rows.splice(rows.indexOf(r!), 1);
      changed.push(...(targets as Row[]));
      break;
    }
    case "activity.validate": targets.forEach((r) => set(r!, { validated_by: 1, validated_at: now })); break;
    case "activity.unvalidate": targets.forEach((r) => set(r!, { validated_by: null, validated_at: null })); break;
    case "invoice.mark_invoiced": targets.forEach((r) => set(r!, { invoiced_at: now, invoiced_by: 1 })); break;
    case "invoice.back_to_draft": targets.forEach((r) => set(r!, { invoiced_at: null, invoiced_by: null })); break;
    case "invoice.print": targets.forEach((r) => set(r!, { pdf_file_id: pdf(r!) })); break;
    case "invoice.export": targets.forEach((r) => { db.delivery.push({ id: db.delivery.length + 1, connection_id: 1, kind: "invoice", root_table: "invoice", root_id: r!.id, payload: {}, created_at: now, attempts: 1, last_detail: "200 OK" }); changed.push(r!); }); break;
    case "expense.attach_receipt": targets.forEach((r) => { set(r!, { receipt_file_id: fields.receipt_file_id }); setTimeout(() => ocr(r!), 600); }); break;
    case "run.cancel": targets.forEach((r) => set(r!, { cancel_requested: true })); break;
    case "delivery.retry": case "connection.pull": targets.forEach((r) => changed.push(r!)); break;
    case "invoice.run": run = startRun(fx, fields); break;
  }
  idx.delete(tbl.name);
  byInvoice = null;
  allowed = null;
  const u = uniques[tbl.name];
  const dup = u && changed.find((r) => rows.some((o) => o !== r && rows.includes(r) && u[0].every((k) => o[k] === r[k])));
  const failed = dup ? [] : check(c.ensure, changed.filter((r) => rows.includes(r)));
  if (dup || failed.length) {
    if (c.kind === "create") rows.splice(rows.indexOf(changed[0]), 1);
    for (const r of targets) Object.assign(r!, before.get(r!.id));
    idx.delete(tbl.name);
    byInvoice = null;
    return dup ? problem(409, "constraint", { constraint: u[1] }) : problem(409, "rejected", { action: c.ensure, rejected: failed });
  }
  const call = randomUUID();
  for (const r of changed) {
    const o = before.get(r.id) ?? {};
    const n = rows.includes(r) ? r : {};
    const changes = Object.fromEntries(Object.keys({ ...o, ...n }).filter((k) => JSON.stringify(o[k]) !== JSON.stringify(n[k])).map((k) => [k, [o[k] ?? null, n[k] ?? null]]));
    events.push({ at: now, command: name, principal_id: 1, for_id: null, tbl: tbl.name, row_id: r.id, root_table: tbl.root, root_id: rootOf(r)?.id ?? r.id, changes });
  }
  const rootIds = [...new Set(changed.map((r) => (rootOf(r) ?? r).id as number))];
  if (changed.length) broadcast(tbl.root, rootIds, name, call);
  return { status: 200, body: { changed: changed.length ? [{ table: tbl.name, ids: changed.map((r) => r.id) }] : [], ids: rootIds, run, call } };
}

// The ocr.read job of SPEC §17, as the ocr service principal: expense.apply_ocr in its own call.
function ocr(e: Row) {
  const o = { ocr_amount: e.ocr_amount, amount: e.amount };
  Object.assign(e, { ocr_amount: 42.5, amount: e.amount ?? 42.5 });
  const changes = Object.fromEntries(Object.entries(o).filter(([k, v]) => v !== e[k]).map(([k, v]) => [k, [v, e[k]]]));
  events.push({ at: new Date().toISOString(), command: "expense.apply_ocr", principal_id: 2, for_id: null, tbl: "expense", row_id: e.id, root_table: "activity", root_id: e.activity_id, changes });
  broadcast("activity", [e.activity_id as number], "expense.apply_ocr");
}

function broadcast(t: string, ids: number[] | undefined, c: string, e = randomUUID()) {
  const data = `data: ${JSON.stringify({ t, ids, c, e })}\n\n`;
  for (const res of clients) res.write(data);
}

function startRun(fx: Fx, input: Row) {
  const { db } = fx;
  const projects = [...new Set(db.activity.filter((a) => a.invoice_id == null && inRange(a, input)).map((a) => get(fx, "folder", a.folder_id)!.project_id))];
  const id = db.run.length + 1;
  const run: Row = { id, name: "invoice.run", input, state: "queued", total: projects.length, done: 0, rejected: [], error: null, cancel_requested: false, created_at: new Date().toISOString(), finished_at: null };
  db.run.push(run);
  const created: Row[] = [];
  const tick = setInterval(() => {
    if (run.cancel_requested) {
      for (const r of created) db.invoice.splice(db.invoice.indexOf(r), 1);
      Object.assign(run, { state: "cancelled", finished_at: new Date().toISOString() });
    } else {
      run.state = "running";
      for (const pid of projects.slice(run.done as number, (run.done as number) + 10)) {
        const pr = get(fx, "project", pid)!;
        if (pr.entry_closed) (run.rejected as Row[]).push({ id: pid, conditions: ["entry_open"], row: pr });
        else created.push(db.invoice[db.invoice.push({ id: Math.max(...db.invoice.map((v) => v.id as number)) + 1, project_id: pid, date_from: input.date_from, date_to: input.date_to, invoiced_at: null, invoiced_by: null, pdf_file_id: null }) - 1]);
      }
      run.done = Math.min(projects.length, (run.done as number) + 10);
      if (run.done === projects.length) Object.assign(run, { state: "done", finished_at: new Date().toISOString() });
    }
    idx.delete("invoice");
    byInvoice = null;
    broadcast("run", [id], "invoice.run");
    if (run.state === "done" || run.state === "cancelled") {
      clearInterval(tick);
      broadcast("invoice", undefined, "invoice.run");
    }
  }, 100);
  return id;
}

function pdf(inv: Row) {
  const text = `Facture ${inv.id} - ${inv.date_from} - ${inv.date_to}`;
  const objs = ["<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${text.length + 35} >>\nstream\nBT /F1 18 Tf 72 760 Td (${text}) Tj ET\nendstream`, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"];
  let out = "%PDF-1.4\n";
  const offs = objs.map((o, i) => { const at = out.length; out += `${i + 1} 0 obj\n${o}\nendobj\n`; return at; });
  const x = out.length;
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offs.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}trailer << /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${x}\n%%EOF`;
  const data = Buffer.from(out, "latin1");
  const hash = createHash("sha256").update(data).digest("hex");
  blobs.set(hash, { type: "application/pdf", data });
  return register(hash, `facture-${inv.id}.pdf`, "application/pdf");
}
