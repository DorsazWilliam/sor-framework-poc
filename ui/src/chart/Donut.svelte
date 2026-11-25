<script lang="ts">
  import { arc, pie, type PieArcDatum } from "d3-shape";
  import type { Row } from "../api/contract";
  import Frame from "./Frame.svelte";
  import { color, num, type Axis, type Pick, type Series } from "./chart";
  import { t } from "../labels/i18n.svelte";

  // Parts of a whole: five at most, the smallest merged into « Autres ».
  let { rows, x, value, title, onpick, height = 200 }: { rows: Row[]; x: Axis; value: Series; title: string; onpick?: Pick; height?: number } =
    $props();

  type Part = { label: string; v: number; row?: Row };
  const parts = $derived.by(() => {
    const all: Part[] = rows.map((r) => ({ label: x.fmt(r[x.key]), v: num(r, value.key), row: r })).sort((a, b) => b.v - a.v);
    if (all.length <= 5) return all;
    return [...all.slice(0, 4), { label: t("ui.others"), v: all.slice(4).reduce((s, p) => s + p.v, 0) }];
  });
  const total = $derived(parts.reduce((s, p) => s + p.v, 0));
  const pct = (v: number) => `${Math.round((100 * v) / (total || 1))} %`;
  const arcs = $derived(pie<Part>().value((p) => Math.max(0, p.v)).sort(null)(parts));
  const r = $derived(height / 2 - 2);
  const shape = $derived(arc<PieArcDatum<Part>>().innerRadius(r * 0.62).outerRadius(r).padAngle(0.01));
</script>

<Frame {title} {height} legend={parts.map((p, i) => ({ label: `${p.label} ${pct(p.v)}`, color: color(i) }))}>
  {#snippet children(w, mark)}
    <g transform="translate({w / 2},{height / 2})">
      {#each arcs as a, i (i)}
        {@const [cx, cy] = shape.centroid(a)}
        <path
          d={shape(a)}
          fill={color(i)}
          {@attach mark(`${a.data.label} · ${value.fmt(a.data.v)} (${pct(a.data.v)})`, w / 2 + cx, height / 2 + cy, a.data.row && onpick && (() => onpick(a.data.row!)))}
        />
      {/each}
      <text class="total" text-anchor="middle" dominant-baseline="middle">{value.fmt(total)}</text>
    </g>
  {/snippet}
</Frame>

<style>
  .total {
    fill: var(--text);
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
  }
</style>
