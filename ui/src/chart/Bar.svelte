<script lang="ts">
  import { scaleBand } from "d3-scale";
  import type { Row } from "../api/contract";
  import Frame from "./Frame.svelte";
  import Axis from "./Axis.svelte";
  import { clip, color, num, textWidth, valueScale, type Axis as AxisT, type Pick, type Series } from "./chart";

  // Bars by axis value: one per series side by side, or stacked; vertical, or horizontal (categories on the left).
  let {
    rows,
    x,
    series,
    title,
    horizontal = false,
    stacked = false,
    onpick,
    height = 200,
  }: { rows: Row[]; x: AxisT; series: Series[]; title: string; horizontal?: boolean; stacked?: boolean; onpick?: Pick; height?: number } =
    $props();

  // Value span of each bar: side by side from 0, stacked from the sum of the series before it (by sign).
  const spans = $derived(
    rows.map((r) => {
      let pos = 0;
      let neg = 0;
      return series.map((s) => {
        const v = num(r, s.key);
        if (!stacked) return [Math.min(0, v), Math.max(0, v), v];
        const from = v < 0 ? neg : pos;
        if (v < 0) neg += v;
        else pos += v;
        return [Math.min(from, from + v), Math.max(from, from + v), v];
      });
    }),
  );
  const value = $derived(valueScale(spans.flat(2), series[0]));
  const labels = $derived(rows.map((r) => x.fmt(r[x.key])));

  function layout(w: number) {
    const top = 6;
    const bottom = 20;
    const left = horizontal ? Math.min(w * 0.35, Math.max(...labels.map(textWidth), 20) + 10) : Math.max(...value.ticks.map((t) => textWidth(t.label)), 16) + 10;
    const right = horizontal ? 16 : 4;
    const band = scaleBand<number>()
      .domain(rows.map((_, i) => i))
      .range(horizontal ? [top, height - bottom] : [left, w - right])
      .paddingInner(0.25)
      .paddingOuter(0.1);
    const inner = scaleBand<number>()
      .domain(stacked ? [0] : series.map((_, j) => j))
      .range([0, band.bandwidth()])
      .paddingInner(0.08);
    const v = value.s.copy().range(horizontal ? [left, w - right] : [height - bottom, top]);
    const bars = rows.flatMap((r, i) =>
      series.map((s, j) => {
        const [lo, hi, raw] = spans[i][j];
        const o = band(i)! + inner(stacked ? 0 : j)!;
        const a = v(horizontal ? lo : hi);
        const len = Math.abs(v(hi) - v(lo));
        const text = `${labels[i]} · ${series.length > 1 ? `${s.label} ` : ""}${s.fmt(raw)}`;
        return horizontal
          ? { x: a, y: o, w: len, h: inner.bandwidth(), fill: color(j), text, tx: a + len / 2, ty: o, row: r }
          : { x: o, y: a, w: inner.bandwidth(), h: len, fill: color(j), text, tx: o + inner.bandwidth() / 2, ty: a, row: r };
      }),
    );
    // category labels: every k-th when they do not all fit
    const room = horizontal ? band.step() / 14 : band.step() / (Math.max(...labels.map(textWidth), 1) + 8);
    const k = Math.max(1, Math.ceil(1 / room));
    const cats = labels.flatMap((l, i) =>
      i % k ? [] : [{ pos: band(i)! + band.bandwidth() / 2, label: horizontal ? clip(l, left - 10) : l }],
    );
    const ticks = value.ticks.map((t) => ({ pos: v(t.v), label: t.label }));
    return { left, top, bottom, right, bars, cats, ticks, zero: v(0) };
  }
</script>

<Frame {title} {height} legend={series.length > 1 ? series.map((s, j) => ({ label: s.label, color: color(j) })) : []}>
  {#snippet children(w, mark)}
    {@const g = layout(w)}
    {#if horizontal}
      <Axis side="bottom" at={height - g.bottom} ticks={g.ticks} grid={height - g.bottom - g.top} />
      <Axis side="left" at={g.left} ticks={g.cats} />
    {:else}
      <Axis side="left" at={g.left} ticks={g.ticks} grid={w - g.left - g.right} />
      <Axis side="bottom" at={height - g.bottom} ticks={g.cats} />
    {/if}
    {#each g.bars as b, i (i)}
      <rect x={b.x} y={b.y} width={Math.max(b.w, 0)} height={Math.max(b.h, 0)} fill={b.fill} rx="1" {@attach mark(b.text, b.tx, b.ty, onpick && (() => onpick(b.row)))} />
    {/each}
    {#if horizontal}
      <line class="zero" x1={g.zero} x2={g.zero} y1={g.top} y2={height - g.bottom} />
    {:else}
      <line class="zero" x1={g.left} x2={w - g.right} y1={g.zero} y2={g.zero} />
    {/if}
  {/snippet}
</Frame>

<style>
  .zero {
    stroke: var(--border-strong);
  }
</style>
