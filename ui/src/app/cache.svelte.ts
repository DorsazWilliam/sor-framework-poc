import { untrack } from "svelte";
import type { Labels, Lang, Me, Meta, Row, ViewBody } from "../api/contract";
import { api } from "../api/client";

// Client view cache (SPEC §20, D43): every view read goes through it. A cached result is shown at once; it is read
// again unless fresh: read while the live stream was up, and no live key on one of its roots since. Entries, me, meta
// and labels are kept in sessionStorage: a reload paints from them, then everything is read again.
type Entry = { rows: Row[]; at: number; roots: string[]; epoch: number; size: number };
// A view, its body, and roots the view's own do not name (a read keyed by a table in its params).
export type Read = [view: string, body: ViewBody, roots?: string[]];
export type Boot = { me: Me; meta: Meta; lang: Lang; labels: Labels };

const PREFIX = "sor:";
const LIMIT = 2_500_000; // characters of rows kept, the oldest entries evicted first

const entries = new Map<string, Entry>(); // in read order
const inflight = new Map<string, { p: Promise<Row[]>; roots: string[] }>();
const subs = new Set<{ roots: string[]; rerun: () => void }>();
const touched = new Map<string, number>();
let viewRoots = new Map<string, string[]>();
let self = 0;
let size = 0;
let seq = 0;
let epoch = 0; // bumped when the stream opens: what was read before is stale
let up = false;
let persist = true;

export const reads = $state({ pending: 0 });

function put(k: string, v: string) {
  if (!persist) return;
  try {
    sessionStorage.setItem(PREFIX + k, v);
  } catch {
    persist = false;
  }
}
function get(k: string) {
  try {
    return sessionStorage.getItem(PREFIX + k);
  } catch {
    return null;
  }
}
function remove(k: string) {
  try {
    sessionStorage.removeItem(PREFIX + k);
  } catch {
    // storage unavailable: nothing kept
  }
}

export function clear() {
  entries.clear();
  inflight.clear();
  size = 0;
  try {
    for (const k of Object.keys(sessionStorage)) if (k.startsWith(PREFIX)) sessionStorage.removeItem(k);
  } catch {
    // storage unavailable: nothing kept
  }
}

export function restore(): Boot | null {
  try {
    return JSON.parse(get("boot") ?? "null");
  } catch {
    return null;
  }
}

export const remember = (b: Boot) => put("boot", JSON.stringify(b));

// The identity and registry the entries are read with; the entries kept by an earlier page of this tab come back stale.
export function begin(me: Me, meta: Meta) {
  self = me.self;
  viewRoots = new Map(meta.views.map((v) => [v.name, v.roots]));
  const kept: [string, Entry][] = [];
  try {
    for (const k of Object.keys(sessionStorage)) {
      if (!k.startsWith(`${PREFIX}v:`)) continue;
      const s = sessionStorage.getItem(k)!;
      const { rows, at, roots } = JSON.parse(s);
      kept.push([k.slice(PREFIX.length + 2), { rows, at, roots, epoch: -1, size: s.length }]);
    }
  } catch {
    // storage unavailable: nothing kept
  }
  for (const [k, e] of kept.sort((a, b) => a[1].at - b[1].at)) {
    entries.set(k, e);
    size += e.size;
  }
}

function forget(k: string) {
  const e = entries.get(k);
  if (!e) return;
  size -= e.size;
  entries.delete(k);
  remove(`v:${k}`);
}

function store(k: string, rows: Row[], roots: string[], ep: number) {
  const at = Date.now();
  const s = JSON.stringify({ rows, at, roots });
  forget(k);
  entries.set(k, { rows, at, roots, epoch: ep, size: s.length });
  size += s.length;
  for (const [old] of entries) {
    if (size <= LIMIT) break;
    forget(old);
  }
  if (entries.has(k)) put(`v:${k}`, s);
}

const isFresh = (e: Entry | undefined) => !!e && up && e.epoch === epoch;

function fetchRows(k: string, view: string, body: ViewBody, roots: string[]): Promise<Row[]> {
  const known = inflight.get(k);
  if (known) return known.p;
  const ep = up ? epoch : -1;
  const s0 = seq;
  reads.pending++;
  const p = api
    .view(view, body)
    .then((rows) => {
      // a key on one of its roots during the read: the rows may predate it, the reader of the key reads again
      if (roots.every((r) => (touched.get(r) ?? 0) <= s0)) store(k, rows, roots, ep === epoch ? ep : -1);
      return rows;
    })
    .finally(() => {
      if (inflight.get(k)?.p === p) inflight.delete(k);
      reads.pending--;
    });
  inflight.set(k, { p, roots });
  return p;
}

const rootsOf = (rs: Read[]) => [...new Set(rs.flatMap(([v, , extra]) => [...(viewRoots.get(v) ?? []), ...(extra ?? [])]))];

// Shows the cached rows of the reads at once when all are cached, then the server's unless all were fresh.
// Often called from an effect: nothing it reads (the cached rows shown, the pending count) is the effect's dependency.
export const load = (rs: Read[], show: (rows: Row[][]) => unknown): Promise<void> =>
  untrack(() => {
    const qs = rs.map(([v, body, extra]) => ({ k: `${self}|${v}|${JSON.stringify(body)}`, v, body, roots: rootsOf([[v, body, extra]]) }));
    const cached = qs.map((q) => entries.get(q.k));
    if (cached.every(Boolean)) {
      show(cached.map((e) => e!.rows));
      if (cached.every(isFresh)) return Promise.resolve();
    }
    return Promise.all(qs.map((q, i) => (isFresh(cached[i]) ? cached[i]!.rows : fetchRows(q.k, q.v, q.body, q.roots)))).then((rows) => {
      show(rows);
    });
  });

// The rows as the server has them (fresh cached rows or a read).
export async function read(view: string, body: ViewBody): Promise<Row[]> {
  let out: Row[] = [];
  await load([[view, body]], ([rows]) => (out = rows));
  return out;
}

// Calls rerun when a live key (or one's own command) touches one of the roots, and on a stream reconnect.
export function watch(roots: string[], rerun: () => void) {
  const s = { roots, rerun };
  subs.add(s);
  return () => subs.delete(s);
}

// load, again on every key on the reads' roots, until the returned function is called.
export function follow(rs: Read[], show: (rows: Row[][]) => unknown, fail?: (e: unknown) => void) {
  let on = true;
  const run = () =>
    load(rs, (rows) => on && show(rows)).catch((e) => {
      if (!fail) throw e;
      if (on) fail(e);
    });
  run();
  const off = watch(rootsOf(rs), run);
  return () => {
    on = false;
    off();
  };
}

// Live keys (or one's own command) on these roots: their entries are dropped, the readers on them read again.
export function touch(roots: Set<string>) {
  seq++;
  for (const r of roots) touched.set(r, seq);
  const hit = (rs: string[]) => rs.some((r) => roots.has(r));
  for (const [k, e] of entries) if (hit(e.roots)) forget(k);
  for (const [k, f] of inflight) if (hit(f.roots)) inflight.delete(k);
  for (const s of [...subs]) if (hit(s.roots)) s.rerun();
}

// The stream's state: when it opens, everything read before is stale; on a reopen, the readers read again.
export function online(on: boolean) {
  up = on;
  if (!on) return;
  inflight.clear();
  if (epoch++) for (const s of [...subs]) s.rerun();
}
