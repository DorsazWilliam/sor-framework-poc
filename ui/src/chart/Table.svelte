<script lang="ts">
  import type { Col, Row } from "../api/contract";
  import { display, isNumeric } from "../app/display";
  import type { Pick } from "./chart";

  // The rows as a small table, cells formatted as in the grid.
  let { rows, columns, label, title, onpick }: { rows: Row[]; columns: Col[]; label: (c: Col) => string; title: string; onpick?: Pick } = $props();
</script>

<div class="wrap">
  <table aria-label={title}>
    <thead>
      <tr>{#each columns as c (c.name)}<th class:num={isNumeric(c)}>{label(c)}</th>{/each}</tr>
    </thead>
    <tbody>
      {#each rows as r, i (i)}
        <tr class:pick={!!onpick} onclick={() => onpick?.(r)}>
          {#each columns as c (c.name)}
            {@const v = r[c.name]}
            <td class:num={isNumeric(c)} class:neg={typeof v === "number" && v < 0 && isNumeric(c)}>{display(c, v)}</td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .wrap {
    max-height: 220px;
    overflow: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-grid);
    font-variant-numeric: tabular-nums;
  }
  th,
  td {
    height: var(--h-row);
    padding: var(--cell-pad);
    border-bottom: var(--bw) solid var(--border);
    text-align: left;
    white-space: nowrap;
  }
  th {
    position: sticky;
    top: 0;
    background: var(--header);
    font-weight: var(--fw-semibold);
  }
  .num {
    text-align: right;
  }
  .neg {
    color: var(--negative);
  }
  .pick {
    cursor: pointer;
  }
  .pick:hover {
    background: var(--hover);
  }
</style>
