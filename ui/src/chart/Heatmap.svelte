<script lang="ts">
  import type { Row } from "../api/contract";
  import Frame from "./Frame.svelte";
  import { num, type Axis, type Pick, type Series } from "./chart";
  import { fmtDate, isoDate } from "../ds/format";
  import { i18n } from "../labels/i18n.svelte";

  // One cell per day over the year ending on the last day of the rows (or today): weeks as columns, Monday first.
  let { rows, x, value, title, onpick }: { rows: Row[]; x: Axis; value: Series; title: string; onpick?: Pick } = $props();

  const [TOP, LEFT, GAP] = [16, 28, 2];
  const byDay = $derived(new Map(rows.map((r) => [String(r[x.key]), r])));
  const max = $derived(Math.max(1e-9, ...rows.map((r) => num(r, value.key))));
  const last = $derived(rows.length ? new Date(`${rows.map((r) => String(r[x.key])).sort().at(-1)}T00:00`) : new Date());
  const days = $derived.by(() => {
    const start = new Date(last.getFullYear(), last.getMonth(), last.getDate() - ((last.getDay() + 6) % 7) - 52 * 7);
    return Array.from({ length: 53 * 7 }, (_, i) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + i)).filter((d) => d <= last);
  });
  const level = (v: number) => (v <= 0 ? 0 : Math.min(4, 1 + Math.floor((4 * v) / max - 1e-9)));
  const month = $derived(new Intl.DateTimeFormat(`${i18n.lang}-CH`, { month: "short" }));
  const weekday = $derived(new Intl.DateTimeFormat(`${i18n.lang}-CH`, { weekday: "short" }));
  const cellOf = (w: number) => Math.max(4, Math.min(14, Math.floor((w - LEFT) / 53) - GAP));
</script>

<Frame {title} height={TOP + 7 * (14 + GAP)}>
  {#snippet children(w, mark)}
    {@const c = cellOf(w)}
    {#each [0, 2, 4] as d (d)}
      <text x={LEFT - 6} y={TOP + d * (c + GAP) + c / 2} text-anchor="end" dominant-baseline="middle">{weekday.format(days[d] ?? last)}</text>
    {/each}
    {#each days as d, i (i)}
      {@const iso = isoDate(d)}
      {@const r = byDay.get(iso)}
      {@const v = r ? num(r, value.key) : 0}
      {@const cx = LEFT + Math.floor(i / 7) * (c + GAP)}
      {#if d.getDate() <= 7 && i % 7 === 0}<text x={cx} y={TOP - 5}>{month.format(d)}</text>{/if}
      <rect
        x={cx}
        y={TOP + (i % 7) * (c + GAP)}
        width={c}
        height={c}
        rx="2"
        fill="var(--heat-{level(v)})"
        {@attach mark(`${fmtDate(iso)} · ${value.fmt(v)}`, cx + c / 2, TOP + (i % 7) * (c + GAP), r && onpick && (() => onpick(r)))}
      />
    {/each}
  {/snippet}
</Frame>

<style>
  text {
    fill: var(--text-muted);
  }
</style>
