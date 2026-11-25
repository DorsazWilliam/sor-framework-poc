import type { Changed, LiveKey } from "../api/contract";
import { table } from "./session.svelte";

type Sub = { roots: string[]; rerun: () => void };

const subs = new Set<Sub>();
const queued: LiveKey[] = [];
// Echo (SPEC §13): while commands of this session are in flight, incoming keys are held; the calls they return
// name the keys that echo a refresh already done, dropped when the last one is answered.
let inflight = 0;
const held: LiveKey[] = [];
const answered = new Set<string>();
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

function receive(key: LiveKey) {
  queued.push(key);
  timer ??= setTimeout(flush, 50);
}

// own runs a command of this session and refreshes what it changed.
export async function own(command: () => Promise<Changed>): Promise<Changed> {
  inflight++;
  try {
    const res = await command();
    answered.add(res.call);
    rerun(new Set(res.changed.map((c) => table(c.table)?.root ?? c.table)));
    return res;
  } finally {
    if (--inflight === 0) {
      for (const key of held.splice(0)) if (!answered.has(key.e)) receive(key);
      answered.clear();
    }
  }
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
    if (inflight) held.push(key);
    else receive(key);
  };
}
