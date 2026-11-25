<script lang="ts">
  import type { Snippet } from "svelte";
  import { marker, type Mark, type Tip } from "./chart";

  // The SVG of a chart at its container's width, its legend and its tooltip.
  let {
    title,
    height,
    legend = [],
    children,
  }: { title: string; height: number; legend?: { label: string; color: string }[]; children: Snippet<[number, Mark]> } = $props();

  let width = $state(0);
  let tip = $state<Tip | null>(null);
  const mark = marker((t) => (tip = t));
</script>

<div class="chart">
  {#if legend.length}
    <ul class="legend">
      {#each legend as l (l.label)}<li><span class="swatch" style:background={l.color}></span>{l.label}</li>{/each}
    </ul>
  {/if}
  <div class="plot" bind:clientWidth={width}>
    {#if width}
      <svg role="img" aria-label={title} style:width="{width}px" style:height="{height}px" viewBox="0 0 {width} {height}">
        <title>{title}</title>
        {@render children(width, mark)}
      </svg>
    {/if}
    {#if tip}
      <div class="tip" role="tooltip" style:left="{Math.min(Math.max(tip.x, 80), width - 80)}px" style:top="{tip.y}px">{tip.text}</div>
    {/if}
  </div>
</div>

<style>
  .chart {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    min-width: 0;
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--s-1) var(--s-5);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--fs-caption);
    color: var(--text-muted);
  }
  .legend li {
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }
  .swatch {
    width: 8px;
    height: 8px;
    border-radius: 2px;
  }
  .plot {
    position: relative;
    min-width: 0;
  }
  svg {
    display: block;
    overflow: visible;
    font-size: var(--fs-caption);
    font-variant-numeric: tabular-nums;
  }
  svg :global(:focus-visible) {
    outline: 1px solid var(--accent);
    outline-offset: 1px;
  }
  .tip {
    position: absolute;
    z-index: 3;
    transform: translate(-50%, calc(-100% - 6px));
    padding: var(--s-1) var(--s-3);
    border: var(--bw) solid var(--border);
    border-radius: var(--r-button);
    background: var(--surface);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fs-caption);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    pointer-events: none;
  }
</style>
