import type { Changed, LiveKey } from "../api/contract";
import { table } from "./session.svelte";

type Sub = { roots: string[]; rerun: () => void };

const subs = new Set<Sub>();
const pending = new Set<Sub>();
let timer: ReturnType<typeof setTimeout> | undefined;

export const live = $state({ state: "connecting" as "connecting" | "live" | "offline" });

export function watch(roots: string[], rerun: () => void) {
  const s = { roots, rerun };
  subs.add(s);
  return () => {
    subs.delete(s);
    pending.delete(s);
  };
}

function flush() {
  timer = undefined;
  const due = [...pending];
  pending.clear();
  for (const s of due) s.rerun();
}

function touched(roots: Set<string>, now: boolean) {
  for (const s of subs) if (s.roots.some((r) => roots.has(r))) pending.add(s);
  if (now) flush();
  else timer ??= setTimeout(flush, 50);
}

export function applied(res: Changed) {
  touched(new Set(res.changed.map((c) => table(c.table)?.root ?? c.table)), true);
}

export function connect() {
  let down = false;
  const es = new EventSource("/api/live");
  es.onopen = () => {
    if (down) for (const s of subs) s.rerun();
    down = false;
    live.state = "live";
  };
  es.onerror = () => {
    down = true;
    live.state = "offline";
    if (es.readyState === EventSource.CLOSED) setTimeout(connect, 3000);
  };
  es.onmessage = (e) => touched(new Set([(JSON.parse(e.data) as LiveKey).t]), false);
}
