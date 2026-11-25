import type { Changed, LiveKey } from "../api/contract";
import { table } from "./session.svelte";

type Sub = { roots: string[]; rerun: () => void };

const subs = new Set<Sub>();
const queued: LiveKey[] = [];
// Calls of this session: their live keys are an echo of a refresh already done by `applied`.
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

// Keys wait for the debounce, so an echo arriving before its command's response is still recognized.
function flush() {
  timer = undefined;
  const keys = queued.splice(0).filter((k) => !own.has(k.e));
  if (keys.length) rerun(new Set(keys.map((k) => k.t)));
}

export function applied(res: Changed) {
  own.add(res.call);
  if (own.size > 200) own.delete(own.values().next().value!);
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
    queued.push(JSON.parse(e.data) as LiveKey);
    timer ??= setTimeout(flush, 50);
  };
}
