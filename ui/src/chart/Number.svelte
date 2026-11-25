<script lang="ts">
  // One value, and an optional secondary one below it.
  let { value, secondary }: { value: string; secondary?: string } = $props();
</script>

<div class="number">
  <span class="value">{value}</span>
  {#if secondary}<span class="secondary">{secondary}</span>{/if}
</div>

<style>
  .number {
    display: flex;
    flex-direction: column;
    gap: var(--s-1);
    font-variant-numeric: tabular-nums;
  }
  .value {
    font-size: 28px;
    font-weight: var(--fw-semibold);
    line-height: 1.2;
  }
  .secondary {
    color: var(--text-muted);
  }
</style>
