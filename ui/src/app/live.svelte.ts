import type { Changed, LiveKey } from "../api/contract";
import { online, touch } from "./cache.svelte";
import { table } from "./session.svelte";

const queued: LiveKey[] = [];
// Echo (SPEC §13): while commands of this session are in flight, incoming keys are held; the calls they return
// name the keys that echo a refresh already done, dropped when the last one is answered.
let inflight = 0;
const held: LiveKey[] = [];
const answered = new Set<string>();
let timer: ReturnType<typeof setTimeout> | undefined;

export const live = $state({ state: "connecting" as "connecting" | "live" | "offline" });

function flush() {
  timer = undefined;
  touch(new Set(queued.splice(0).map((k) => k.t)));
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
    touch(new Set(res.changed.map((c) => table(c.table)?.root ?? c.table)));
    return res;
  } finally {
    if (--inflight === 0) {
      for (const key of held.splice(0)) if (!answered.has(key.e)) receive(key);
      answered.clear();
    }
  }
}

export function connect() {
  const es = new EventSource("/api/live");
  es.onopen = () => {
    online(true);
    live.state = "live";
  };
  es.onerror = () => {
    online(false);
    live.state = "offline";
    if (es.readyState === EventSource.CLOSED) setTimeout(connect, 3000);
  };
  es.onmessage = (e) => {
    const key = JSON.parse(e.data) as LiveKey;
    if (inflight) held.push(key);
    else receive(key);
  };
}
