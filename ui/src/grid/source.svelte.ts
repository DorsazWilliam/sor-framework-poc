import { SvelteMap } from "svelte/reactivity";
import type { Agg, Filter, Row, Sort } from "../api/contract";
import { api } from "../api/client";

export const PAGE = 200;
export const ROW = 24;
export const GROUP = 26;
const BULK = 5000;

export type Query = {
  view: string; params: Record<string, unknown>; filter: Filter[]; sort: Sort[]; group: string[]; agg: Agg[];
  count: string; // column passed to the count aggregate
};

export type Entry =
  | { kind: "row"; row: Row; depth: number }
  | { kind: "group"; row: Row; key: string; open: boolean; count: number }
  | undefined;

// A filtered, sorted row list read by pages of PAGE rows, cached by offset.
export class Pager {
  count = $state(0);
  #version = $state(0);
  #pages = new Map<number, Row[]>();
  #stale = new Map<number, Row[]>();
  #loading = new Set<number>();
  #ids = new Map<unknown, Row>();

  constructor(
    private q: Query,
    private filter: Filter[],
    count: number,
  ) {
    this.count = count;
  }

  rowAt(i: number) {
    void this.#version;
    const p = Math.floor(i / PAGE);
    return (this.#pages.get(p) ?? this.#stale.get(p))?.[i % PAGE];
  }

  byId(id: unknown) {
    void this.#version;
    return this.#ids.get(id);
  }

  #set(p: number, rows: Row[]) {
    this.#pages.set(p, rows);
    for (const r of rows) this.#ids.set(r.id, r);
  }

  ensure(from: number, to: number) {
    const a = Math.floor(Math.max(0, from) / PAGE);
    const b = Math.floor(Math.min(to, this.count - 1) / PAGE);
    for (let p = a; p <= b; p++) if (!this.#pages.has(p) && !this.#loading.has(p)) this.#load(p);
  }

  async #load(p: number) {
    const loading = this.#loading;
    const pages = this.#pages;
    loading.add(p);
    try {
      const { view, params, sort } = this.q;
      const rows = await api.view(view, { params, filter: this.filter, sort, limit: PAGE, offset: p * PAGE });
      if (pages !== this.#pages) return;
      this.#set(p, rows);
      this.#version++;
    } finally {
      loading.delete(p);
    }
  }

  async all(): Promise<Row[]> {
    const { view, params, sort } = this.q;
    for (let off = 0; off < this.count; off += BULK) {
      let missing = false;
      for (let p = off / PAGE; p < (off + BULK) / PAGE && p * PAGE < this.count; p++) missing ||= !this.#pages.has(p);
      if (!missing) continue;
      const rows = await api.view(view, { params, filter: this.filter, sort, limit: BULK, offset: off });
      for (let i = 0; i < rows.length; i += PAGE) this.#set((off + i) / PAGE, rows.slice(i, i + PAGE));
    }
    this.#version++;
    return [...this.#pages.keys()].sort((a, b) => a - b).flatMap((p) => this.#pages.get(p)!);
  }

  reset(count: number) {
    this.#stale = this.#pages;
    this.#pages = new Map();
    this.#loading = new Set();
    this.count = count;
    this.#version++;
  }
}

const aggs = (q: Query): Agg[] => [...q.agg.filter((a) => a.fn !== "count"), { col: q.count, fn: "count" }];
const keyOf = (q: Query, r: Row) => JSON.stringify(q.group.map((g) => r[g]));

// The rows of a view under a wrap, flat or grouped one level (drill-down loads a group's rows).
export class Source {
  q: Query;
  totals = $state.raw<Row | null>(null);
  groups = $state.raw<Row[]>([]);
  flat = $state.raw<Pager | null>(null);
  open = new SvelteMap<string, Pager>();
  ready = $state(false);
  #gen = 0;

  constructor(q: Query) {
    this.q = q;
    this.refresh();
  }

  get grouped() {
    return this.q.group.length > 0;
  }

  // Grouped layout: first entry index and first pixel of each group block.
  #layout = $derived.by(() => {
    const idx: number[] = [];
    const px: number[] = [];
    let i = 0;
    let y = 0;
    for (const g of this.groups) {
      idx.push(i);
      px.push(y);
      const n = this.open.get(keyOf(this.q, g))?.count ?? 0;
      i += 1 + n;
      y += GROUP + n * ROW;
    }
    return { idx, px, length: i, height: y };
  });

  get length() {
    return this.grouped ? this.#layout.length : (this.flat?.count ?? 0);
  }

  get height() {
    return this.grouped ? this.#layout.height : this.length * ROW;
  }

  #block(i: number) {
    const { idx } = this.#layout;
    let lo = 0;
    let hi = idx.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (idx[mid] <= i) lo = mid;
      else hi = mid - 1;
    }
    return lo;
  }

  entry(i: number): Entry {
    if (!this.grouped) {
      const row = this.flat?.rowAt(i);
      return row && { kind: "row", row, depth: 0 };
    }
    const b = this.#block(i);
    const g = this.groups[b];
    if (!g) return undefined;
    const key = keyOf(this.q, g);
    const pager = this.open.get(key);
    const local = i - this.#layout.idx[b];
    if (local === 0) return { kind: "group", row: g, key, open: !!pager, count: g.count as number };
    const row = pager?.rowAt(local - 1);
    return row && { kind: "row", row, depth: 1 };
  }

  top(i: number) {
    if (!this.grouped) return i * ROW;
    const b = this.#block(i);
    const local = i - this.#layout.idx[b];
    return this.#layout.px[b] + (local === 0 ? 0 : GROUP + (local - 1) * ROW);
  }

  heightOf(i: number) {
    return this.grouped && this.entry(i)?.kind === "group" ? GROUP : ROW;
  }

  indexAt(y: number) {
    if (!this.grouped) return Math.max(0, Math.min(this.length - 1, Math.floor(y / ROW)));
    const { idx, px } = this.#layout;
    if (!idx.length) return 0;
    let lo = 0;
    let hi = px.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (px[mid] <= y) lo = mid;
      else hi = mid - 1;
    }
    const local = y - px[lo];
    const n = this.open.get(keyOf(this.q, this.groups[lo]))?.count ?? 0;
    return idx[lo] + (local < GROUP ? 0 : Math.min(n, 1 + Math.floor((local - GROUP) / ROW)));
  }

  ensure(from: number, to: number) {
    if (!this.grouped) return this.flat?.ensure(from, to);
    for (let b = this.#block(Math.max(0, from)); b < this.groups.length && this.#layout.idx[b] <= to; b++) {
      const start = this.#layout.idx[b] + 1;
      this.open.get(keyOf(this.q, this.groups[b]))?.ensure(from - start, to - start);
    }
  }

  #childFilter(g: Row): Filter[] {
    return [
      ...this.q.filter,
      ...this.q.group.map((c): Filter => (g[c] == null ? { col: c, op: "null" } : { col: c, op: "eq", value: g[c] })),
    ];
  }

  toggle(key: string) {
    if (this.open.has(key)) return this.open.delete(key);
    const g = this.groups.find((r) => keyOf(this.q, r) === key);
    if (g) this.open.set(key, new Pager(this.q, this.#childFilter(g), g.count as number));
  }

  byId(id: unknown) {
    if (!this.grouped) return this.flat?.byId(id);
    for (const p of this.open.values()) {
      const r = p.byId(id);
      if (r) return r;
    }
  }

  async all(): Promise<Row[]> {
    const pagers = this.grouped ? [...this.open.values()] : this.flat ? [this.flat] : [];
    return (await Promise.all(pagers.map((p) => p.all()))).flat();
  }

  // Re-run the wrap (first load, live key, own command): totals, groups, then visible pages.
  async refresh() {
    const gen = ++this.#gen;
    const { view, params, filter, group, sort } = this.q;
    const agg = aggs(this.q);
    const [totals, groups] = await Promise.all([
      api.view(view, { params, filter, agg }),
      group.length
        ? api.view(view, { params, filter, group, agg, sort: group.map((col) => ({ col, dir: "asc" as const })), limit: BULK })
        : Promise.resolve([]),
    ]);
    if (gen !== this.#gen) return;
    this.totals = totals[0] ?? null;
    const count = (this.totals?.count as number) ?? 0;
    if (group.length) {
      this.groups = groups;
      for (const [key, pager] of this.open) {
        const g = groups.find((r) => keyOf(this.q, r) === key);
        if (g) pager.reset(g.count as number);
        else this.open.delete(key);
      }
    } else if (this.flat) this.flat.reset(count);
    else this.flat = new Pager(this.q, filter, count);
    this.ready = true;
  }
}
