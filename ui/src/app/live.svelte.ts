import type { Changed, LiveKey } from "../api/contract";
import { table } from "./session.svelte";

type Sub = { roots: string[]; rerun: () => void };

const subs = new Set<Sub>();
const queued: LiveKey[] = [];
// Invocation ids (Sor-Call) of this session's commands: their live keys echo a refresh already done by `applied`.
const own = new Set<string>();
let timer: ReturnType<typeof setTimeout> | undefined;

export const live = $state({ state: "connecting" as "connecting" | "live" | "offline" });

export function watch(roots: string[], rerun: () => void) {
  const s = { roots, rerun };
  subs.add(s);
  return () => subs.delete(s);
}

function rerun(roots: Set<string>) {
  for (const s of [...subs]) if (s.roots.some((r) => roots.has(r))) s.rerun();
}

function flush() {
  timer = undefined;
  rerun(new Set(queued.splice(0).map((k) => k.t)));
}

export function newCall() {
  const id = crypto.randomUUID();
  own.add(id);
  if (own.size > 200) own.delete(own.values().next().value!);
  return id;
}

export function applied(res: Changed) {
  rerun(new Set(res.changed.map((c) => table(c.table)?.root ?? c.table)));
}

export function connect() {
  let down = false;
  const es = new EventSource("/api/live");
  es.onopen = () => {
    if (down) for (const s of [...subs]) s.rerun();
    down = false;
    live.state = "live";
  };
  es.onerror = () => {
    down = true;
    live.state = "offline";
    if (es.readyState === EventSource.CLOSED) setTimeout(connect, 3000);
  };
  es.onmessage = (e) => {
    const key = JSON.parse(e.data) as LiveKey;
    if (own.has(key.e)) return;
    queued.push(key);
    timer ??= setTimeout(flush, 50);
  };
}
