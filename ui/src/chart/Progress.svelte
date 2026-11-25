<script lang="ts">
  import { scaleLinear } from "d3-scale";
  import type { Row } from "../api/contract";
  import Frame from "./Frame.svelte";
  import { clip, color, num, textWidth, type Axis, type Pick, type Series } from "./chart";

  // Per row, the actual against its target on a common scale: the target as a track, the actual over it, what
  // exceeds the target in the negative color.
  let { rows, x, actual, target, title, onpick }: { rows: Row[]; x: Axis; actual: Series; target: Series; title: string; onpick?: Pick } =
    $props();

  const ROW = 22;
  const height = $derived(rows.length * ROW);
  const items = $derived(
    rows.map((r) => {
      const a = num(r, actual.key);
      const g = num(r, target.key);
      return { r, a, g, label: x.fmt(r[x.key]), value: `${actual.fmt(a)} / ${target.fmt(g)}`, over: a > g };
    }),
  );

  function layout(w: number) {
    const left = Math.min(w * 0.35, Math.max(...items.map((i) => textWidth(i.label)), 20) + 10);
    const right = Math.max(...items.map((i) => textWidth(i.value)), 20) + 10;
    const s = scaleLinear()
      .domain([0, Math.max(1e-9, ...items.flatMap((i) => [i.a, i.g]))])
      .range([left, w - right]);
    return { left, s, w };
  }
</script>

<Frame {title} {height} legend={[{ label: actual.label, color: color(0) }, { label: target.label, color: "var(--border)" }]}>
  {#snippet children(w, mark)}
    {@const g = layout(w)}
    {#each items as it, i (i)}
      {@const y = i * ROW + 4}
      <text class="label" x={g.left - 8} y={y + 7} text-anchor="end" dominant-baseline="middle">{clip(it.label, g.left - 10)}</text>
      <rect class="track" x={g.left} {y} width={g.s(it.g) - g.left} height="14" rx="2" />
      <rect
        x={g.left}
        y={y + 3}
        width={g.s(Math.min(it.a, it.g)) - g.left}
        height="8"
        rx="1"
        fill={color(0)}
        {@attach mark(`${it.label} · ${it.value}`, g.s(it.a), y, onpick && (() => onpick(it.r)))}
      />
      {#if it.over}<rect class="over" x={g.s(it.g)} y={y + 3} width={g.s(it.a) - g.s(it.g)} height="8" rx="1" />{/if}
      <line class="target" x1={g.s(it.g)} x2={g.s(it.g)} y1={y} y2={y + 14} />
      <text class="value" class:over={it.over} x={w - 2} y={y + 7} text-anchor="end" dominant-baseline="middle">{it.value}</text>
    {/each}
  {/snippet}
</Frame>

<style>
  .label {
    fill: var(--text);
  }
  .value {
    fill: var(--text-muted);
  }
  .value.over {
    fill: var(--negative);
  }
  .track {
    fill: var(--surface-subtle);
    stroke: var(--border);
  }
  .over {
    fill: var(--negative);
  }
  .target {
    stroke: var(--text-muted);
  }
</style>
