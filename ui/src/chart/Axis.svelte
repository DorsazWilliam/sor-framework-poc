<script lang="ts">
  // Tick labels on one side of the plot, with grid lines across it (length `grid`) and a base line (length `base`).
  let {
    side,
    at,
    ticks,
    grid = 0,
    base = 0,
    from = 0,
  }: { side: "left" | "bottom"; at: number; ticks: { pos: number; label: string }[]; grid?: number; base?: number; from?: number } = $props();
</script>

<g class="axis">
  {#each ticks as t, i (i)}
    {#if side === "left"}
      {#if grid}<line class="grid" x1={at} x2={at + grid} y1={t.pos} y2={t.pos} />{/if}
      <text x={at - 6} y={t.pos} text-anchor="end" dominant-baseline="middle">{t.label}</text>
    {:else}
      {#if grid}<line class="grid" x1={t.pos} x2={t.pos} y1={at} y2={at - grid} />{/if}
      <text x={t.pos} y={at + 14} text-anchor="middle">{t.label}</text>
    {/if}
  {/each}
  {#if base}
    {#if side === "left"}<line class="base" x1={at} x2={at} y1={from} y2={from + base} />
    {:else}<line class="base" x1={from} x2={from + base} y1={at} y2={at} />{/if}
  {/if}
</g>

<style>
  text {
    fill: var(--text-muted);
  }
  .grid {
    stroke: var(--border);
    stroke-dasharray: 2 3;
  }
  .base {
    stroke: var(--border-strong);
  }
</style>
