<script lang="ts">
  import { follow } from "../app/cache.svelte";
  import { colLabel, table, view } from "../app/session.svelte";
  import { display } from "../app/display";
  import { fmtInstant } from "../ds/format";
  import { t } from "../labels/i18n.svelte";

  let { root, id }: { root: string; id: number } = $props();

  type Event = { at: string; command: string; principal_id: number; for_id: number | null; tbl: string; row_id: number; changes: Record<string, [unknown, unknown]> };
  // principal_id and for_id carry a ref: shown through the lookup store.
  const who = (name: string, v: unknown) => {
    const c = view("history")?.columns.find((x) => x.name === name);
    return c ? display(c, v) : String(v);
  };
  let events = $state<Event[]>([]);

  // The history view reads the event log: the root it is about is named by its params, not by its own roots.
  $effect(() =>
    follow([["history", { params: { root_table: root, root_id: id }, sort: [{ col: "at", dir: "desc" }] }, [root]]], ([rows]) => {
      events = rows as Event[];
    }),
  );

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
        <span class="caption">{fmtInstant(e.at)} · {who("principal_id", e.principal_id)}{e.for_id != null ? ` (${who("for_id", e.for_id)})` : ""}</span>
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
