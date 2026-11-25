<script lang="ts">
  import { scaleTime } from "d3-scale";
  import { line } from "d3-shape";
  import type { Row } from "../api/contract";
  import Frame from "./Frame.svelte";
  import Axis from "./Axis.svelte";
  import { color, num, textWidth, valueScale, type Axis as AxisT, type Pick, type Series } from "./chart";
  import { isoDate } from "../ds/format";

  // Series over a time axis (ISO dates: a day, the Monday of a week, the first day of a month).
  let { rows, x, series, title, onpick, height = 200 }: { rows: Row[]; x: AxisT; series: Series[]; title: string; onpick?: Pick; height?: number } =
    $props();

  const date = (r: Row) => new Date(`${r[x.key]}T00:00`);
  const value = $derived(valueScale(rows.flatMap((r) => series.map((s) => num(r, s.key))), series[0]));

  function layout(w: number) {
    const [top, right, bottom] = [6, 12, 20];
    const left = Math.max(...value.ticks.map((t) => textWidth(t.label)), 16) + 10;
    const xs = scaleTime()
      .domain([date(rows[0]), date(rows[rows.length - 1])])
      .range([left, w - right]);
    const ys = value.s.copy().range([height - bottom, top]);
    const lines = series.map(
      (s) =>
        line<Row>()
          .x((r) => xs(date(r)))
          .y((r) => ys(num(r, s.key)))(rows) ?? "",
    );
    const points = rows.flatMap((r) =>
      series.map((s, j) => ({ x: xs(date(r)), y: ys(num(r, s.key)), fill: color(j), row: r, text: `${x.fmt(r[x.key])} · ${series.length > 1 ? `${s.label} ` : ""}${s.fmt(num(r, s.key))}` })),
    );
    const dates = xs.ticks(Math.max(2, Math.floor((w - left - right) / 90)));
    return {
      left, right, top, bottom, lines, points,
      cats: dates.map((d) => ({ pos: xs(d), label: x.fmt(isoDate(d)) })),
      ticks: value.ticks.map((t) => ({ pos: ys(t.v), label: t.label })),
    };
  }
</script>

<Frame {title} {height} legend={series.length > 1 ? series.map((s, j) => ({ label: s.label, color: color(j) })) : []}>
  {#snippet children(w, mark)}
    {#if rows.length}
      {@const g = layout(w)}
      <Axis side="left" at={g.left} ticks={g.ticks} grid={w - g.left - g.right} />
      <Axis side="bottom" at={height - g.bottom} ticks={g.cats} base={w - g.left - g.right} from={g.left} />
      {#each g.lines as d, j (j)}<path {d} fill="none" stroke={color(j)} stroke-width="1.5" />{/each}
      {#each g.points as p, i (i)}
        <circle cx={p.x} cy={p.y} r="2.5" fill={p.fill} {@attach mark(p.text, p.x, p.y, onpick && (() => onpick(p.row)))} />
      {/each}
    {/if}
  {/snippet}
</Frame>
