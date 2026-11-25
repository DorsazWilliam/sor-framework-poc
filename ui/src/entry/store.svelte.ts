import { SvelteMap } from "svelte/reactivity";
import type { Row } from "../api/contract";
import type { Item } from "../ds/types";
import { api, ApiError } from "../api/client";
import { all } from "../app/lookups.svelte";
import { command, session } from "../app/session.svelte";
import { call, report } from "../screen/commands";
import { isoDate } from "../ds/format";
import { t } from "../labels/i18n.svelte";

// Activity fields edited on a line, as named in activity.create / activity.update.
export const FIELDS = ["start_tod", "duration_min", "folder_id", "task_id", "billed_min", "description"] as const;
type Field = (typeof FIELDS)[number];
export type Draft = {
  start_tod: number | null; duration_min: number | null; folder_id: number | null; task_id: number | null;
  billed_min: number | null; description: string | null;
};

const blank = (): Draft => ({ start_tod: null, duration_min: null, folder_id: null, task_id: null, billed_min: null, description: null });
const fromRow = (r: Row) => Object.fromEntries(FIELDS.map((f) => [f, r[f] ?? null])) as Draft;
const same = (a: unknown, b: unknown) => (a ?? "") === (b ?? "");

export function addDays(iso: string, n: number) {
  const [y, m, d] = iso.split("-").map(Number);
  return isoDate(new Date(y, m - 1, d + n));
}
export const mondayOf = (iso: string) => addDays(iso, -((new Date(`${iso}T12:00`).getDay() + 6) % 7));

// Folder and task lookups, loaded once and filtered in memory; allowed tasks cached per folder.
const picks = { folders: [] as Item[], tasks: new Map<number, Row>(), loading: null as Promise<void> | null };
const allowed = new SvelteMap<number, Item[]>();
const asking = new Set<number>();

export function loadPicks() {
  return (picks.loading ??= Promise.all([all("folder"), all("task")]).then(([f, tk]) => {
    picks.folders = f.map((r) => ({ value: String(r.id), label: String(r.label) }));
    picks.tasks = new Map(tk.map((r) => [r.id as number, r]));
  }));
}
export const folderItems = () => picks.folders;
export const taskLabel = (id: number | null) => (id == null ? "" : String(picks.tasks.get(id)?.label ?? ""));
export const billing = (task: number | null) => (task == null ? undefined : picks.tasks.get(task)?.billing);

export function allowedTasks(folder: number | null): Item[] {
  if (folder == null) return [];
  const items = allowed.get(folder);
  if (!items && !asking.has(folder)) {
    asking.add(folder);
    api
      .view("folder_task_allowed", { params: { folder_id: folder }, limit: 5000 })
      .then((rows) => allowed.set(folder, rows.map((r) => ({ value: String(r.task_id), label: taskLabel(r.task_id as number) }))))
      .finally(() => asking.delete(folder));
  }
  return items ?? [];
}
export const forgetAllowed = () => allowed.clear();

let seq = 0;

// One editable line: the server row (projection) and the user's draft over it.
export class Line {
  uid = ++seq;
  id = $state<number | null>(null);
  row = $state.raw<Row | null>(null);
  draft = $state<Draft>(blank());
  fresh = $state(true); // the empty line at the end of a day
  status = $state<"" | "saving" | "saved" | "refused">("");
  reasons = $state<string[]>([]);
  saved = $state(0);
  rev = $state(0); // remount key: reverting also resets what is being typed

  constructor(
    public date: string,
    row?: Row,
  ) {
    if (row) this.load(row);
  }

  // Server values replace the fields the user has not changed since the previous row.
  load(r: Row) {
    const old = this.row;
    for (const f of FIELDS) if (!old || same(this.draft[f], old[f])) (this.draft as Record<Field, unknown>)[f] = r[f] ?? null;
    this.id = r.id as number;
    this.row = r;
    this.fresh = false;
  }

  get changed(): Field[] {
    const base = this.row ?? {};
    return FIELDS.filter((f) => !same(this.draft[f], base[f]) && (f !== "billed_min" || billing(this.draft.task_id) === "sometimes"));
  }

  revert() {
    this.draft = this.row ? fromRow(this.row) : blank();
    if (this.status === "refused") this.status = "";
    this.reasons = [];
    this.rev++;
  }
}

export class Entry {
  user = $state(session.me.id);
  day = $state(isoDate(new Date()));
  mode = $state<"day" | "week">("day");
  lines = $state<Line[]>([]);
  days = $state.raw<Row[]>([]);
  ready = $state(false);
  monday = $derived(mondayOf(this.day));
  week = $derived(Array.from({ length: 7 }, (_, i) => addDays(this.monday, i)));
  dates = $derived(this.mode === "day" ? [this.day] : this.week);
  #shown = "";
  #gen = 0;

  async refresh() {
    const gen = ++this.#gen;
    const key = `${this.user}:${this.monday}`;
    const params = { user_id: this.user, date_from: this.week[0], date_to: this.week[6] };
    const [rows, days] = await Promise.all([
      api.view("timesheet", { params, limit: 5000 }),
      api.view("timesheet_day", { params }),
      loadPicks(),
    ]);
    if (gen !== this.#gen) return;
    this.#merge(rows, key !== this.#shown);
    this.days = days;
    this.#shown = key;
    this.ready = true;
  }

  // Lines keep their place and their drafts; new rows are inserted by start, removed rows dropped.
  #merge(rows: Row[], reset: boolean) {
    const ids = new Set(rows.map((r) => r.id));
    const next = reset ? [] : this.lines.filter((l) => l.id == null || ids.has(l.id));
    const byId = new Map(next.map((l) => [l.id, l]));
    for (const r of rows) {
      const l = byId.get(r.id as number);
      if (l) {
        l.load(r);
        continue;
      }
      const i = next.findIndex((x) => x.date > r.date! || (x.date === r.date && (x.id == null || (x.row!.start_tod as number) > (r.start_tod as number))));
      next.splice(i < 0 ? next.length : i, 0, new Line(r.date as string, r));
    }
    for (const d of this.week) if (!next.some((l) => l.fresh && l.date === d)) next.push(new Line(d));
    this.lines = next;
  }

  dayRow(date: string) {
    return this.days.find((d) => d.date === date);
  }

  // The fresh line becomes a draft line and a new fresh line opens below it.
  promote(l: Line) {
    if (!l.fresh) return;
    l.fresh = false;
    const i = this.lines.indexOf(l);
    this.lines.splice(i + 1, 0, new Line(l.date));
  }

  freshLine(date: string) {
    return this.lines.find((l) => l.fresh && l.date === date);
  }

  duplicate(l: Line) {
    const f = this.freshLine(l.date)!;
    if (f !== l) {
      f.draft = { ...l.draft, start_tod: null };
      f.rev++;
    }
    return f;
  }

  #refuse(l: Line, e: ApiError, cmd: string) {
    l.status = "refused";
    l.reasons = e.problem.rejected?.[0]?.conditions.map((k) => t(`cond.${k}`)) ?? [];
    if (e.code !== "rejected") report(command(cmd)!, e);
  }

  // One command per save: create with every field, update with the changed fields only.
  async save(l: Line, start: number) {
    if (l.status === "saving") return;
    const d = { ...l.draft, start_tod: l.draft.start_tod ?? start };
    if (d.duration_min == null || d.folder_id == null || d.task_id == null) return;
    const fields =
      l.id == null ? FIELDS.filter((f) => d[f] != null && (f !== "billed_min" || billing(d.task_id) === "sometimes")) : l.changed;
    if (!fields.length) return;
    const input = Object.fromEntries(fields.map((f) => [f, d[f] === "" ? null : d[f]]));
    const name = l.id == null ? "activity.create" : "activity.update";
    l.draft.start_tod = d.start_tod;
    l.status = "saving";
    const res = await call(command(name)!, l.id == null ? { user_id: this.user, date: l.date, ...input } : { ids: [l.id], ...input });
    if (res instanceof ApiError) return this.#refuse(l, res, name);
    if (l.id == null) {
      l.id = res.ids[0];
      this.lines = this.lines.filter((x) => x === l || x.id !== l.id);
    }
    l.row = { ...l.row, ...input, id: l.id };
    l.status = "saved";
    l.reasons = [];
    l.saved++;
  }

  async remove(l: Line) {
    if (l.id == null) {
      if (l.fresh) l.revert();
      else this.lines = this.lines.filter((x) => x !== l);
      return;
    }
    const res = await call(command("activity.delete")!, { ids: [l.id] });
    if (res instanceof ApiError) return this.#refuse(l, res, "activity.delete");
    this.lines = this.lines.filter((x) => x !== l);
  }
}
