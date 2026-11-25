import { SvelteMap } from "svelte/reactivity";
import type { Filter, Row } from "../api/contract";
import type { Item } from "../ds/types";
import { load, watch } from "./cache.svelte";
import { view } from "./session.svelte";

// <table>.lookup views: id → label, loaded by batches of missing ids, dropped on live keys.
const cache = new SvelteMap<string, SvelteMap<number, string>>();
const missing = new Map<string, Set<number>>();
const inflight = new Set<string>();
let scheduled = false;

export function lookup(tbl: string, id: number): string | undefined {
  const label = cache.get(tbl)?.get(id);
  if (label === undefined && !inflight.has(`${tbl}:${id}`)) {
    const ids = missing.get(tbl) ?? new Set<number>();
    ids.add(id);
    missing.set(tbl, ids);
    if (!scheduled) {
      scheduled = true;
      queueMicrotask(loadMissing);
    }
  }
  return label;
}

function labels(tbl: string) {
  let m = cache.get(tbl);
  if (!m) {
    m = new SvelteMap();
    cache.set(tbl, m);
    const v = view(`${tbl}.lookup`);
    if (v) watch(v.roots, () => m!.clear());
  }
  return m;
}

async function fetchLookup(tbl: string, filter: Filter[], limit: number, offset = 0) {
  const m = labels(tbl);
  let out: Row[] = [];
  await load([[`${tbl}.lookup`, { filter, limit, offset }]], ([rows]) => {
    for (const r of rows) m.set(r.id as number, String(r.label));
    out = rows;
  });
  return out;
}

// Every row of a lookup view (with its extra columns), for pickers filtered in memory.
export async function all(tbl: string): Promise<Row[]> {
  const out: Row[] = [];
  for (let offset = 0; ; offset += 5000) {
    const rows = await fetchLookup(tbl, [], 5000, offset);
    out.push(...rows);
    if (rows.length < 5000) return out;
  }
}

function loadMissing() {
  scheduled = false;
  for (const [tbl, ids] of missing) {
    const list = [...ids];
    const m = labels(tbl);
    for (const id of list) inflight.add(`${tbl}:${id}`);
    fetchLookup(tbl, [{ col: "id", op: "in", value: list }], 5000)
      .catch(() => [])
      .finally(() => {
        for (const id of list) {
          inflight.delete(`${tbl}:${id}`);
          if (!m.has(id)) m.set(id, `#${id}`);
        }
      });
  }
  missing.clear();
}

export async function search(tbl: string, q: string): Promise<Item[]> {
  const filter: Filter[] = q
    .split(/\s+/)
    .filter(Boolean)
    .map((k) => ({ col: "label", op: "contains", value: k }));
  const rows = await fetchLookup(tbl, filter, 50);
  return rows.map((r) => ({ value: String(r.id), label: String(r.label) }));
}
