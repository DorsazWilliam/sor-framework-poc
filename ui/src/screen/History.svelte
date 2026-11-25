<script lang="ts">
  import type { Row } from "../api/contract";
  import { api } from "../api/client";
  import { watch } from "../app/live.svelte";
  import { colLabel, table, view } from "../app/session.svelte";
  import { display } from "../app/display";
  import { fmtInstant } from "../ds/format";
  import { t } from "../labels/i18n.svelte";

  let { root, id }: { root: string; id: number } = $props();

  type Event = { at: string; command: string; principal: string; for?: string; tbl: string; row_id: number; changes: Record<string, [unknown, unknown]> };
  let events = $state<Event[]>([]);

  async function load() {
    const rows = await api.view("history", { params: { root_table: root, root_id: id }, sort: [{ col: "at", dir: "desc" }] });
    events = rows as Row[] as Event[];
  }

  $effect(() => {
    load();
    return watch([root, ...(view("history")?.roots ?? [])], load);
  });

  const show = (tbl: string, col: string, v: unknown) => {
    const c = table(tbl)?.columns.find((x) => x.name === col);
    return v == null ? "∅" : c ? display(c, v) || String(v) : JSON.stringify(v);
  };
</script>

<ol class="history">
  {#each events as e, i (i)}
    <li>
      <div class="head">
        <strong>{t(`cmd.${e.command}`)}</strong>
        <span class="caption">{fmtInstant(e.at)} · {e.principal}{e.for ? ` (${e.for})` : ""}</span>
      </div>
      {#if e.tbl !== root}<div class="caption">{t(`t.${e.tbl}`)} #{e.row_id}</div>{/if}
      <dl>
        {#each Object.entries(e.changes) as [col, [o, n]] (col)}
          <dt>{colLabel(col, e.tbl)}</dt>
          <dd><span class="old">{show(e.tbl, col, o)}</span> → {show(e.tbl, col, n)}</dd>
        {/each}
      </dl>
    </li>
  {:else}
    <li class="muted">{t("ui.no_history")}</li>
  {/each}
</ol>

<style>
  .history {
    flex: 1;
    margin: 0;
    padding: var(--panel-pad);
    overflow: auto;
    list-style: none;
  }
  li {
    padding: var(--s-4) 0;
    border-bottom: var(--bw) solid var(--border);
  }
  .head {
    display: flex;
    justify-content: space-between;
    gap: var(--s-4);
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--s-1) var(--s-5);
    margin: var(--s-3) 0 0;
    font-size: var(--fs-grid);
  }
  dt {
    color: var(--text-muted);
  }
  dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .old {
    color: var(--text-muted);
    text-decoration: line-through;
  }
</style>
