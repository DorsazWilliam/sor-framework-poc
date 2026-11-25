<script lang="ts">
  import type { Row, Screen, Table } from "../api/contract";
  import Tabs from "../ds/Tabs.svelte";
  import Form from "./Form.svelte";
  import History from "./History.svelte";
  import { screenComponent } from "./components";
  import { follow } from "../app/cache.svelte";
  import { screenLabel, view } from "../app/session.svelte";
  import { t } from "../labels/i18n.svelte";

  // Drawer content: the form of a row (read through the table's list view), child screens, history.
  let {
    screen,
    tbl,
    id,
    initial,
    tabs = true,
    parent,
    onsaved,
  }: {
    screen: Screen; tbl: Table; id: number | null; initial: Row; tabs?: boolean; parent?: Row; onsaved: (id: number) => void;
  } = $props();

  let row = $state<Row | null>(null);
  let tab = $state("form");
  const list = $derived(view(tbl.name));

  $effect(() => {
    row = null;
    if (id == null || !list) return;
    return follow([[list.name, { filter: [{ col: "id", op: "eq", value: id }] }]], ([rows]) => (row = rows[0] ?? null));
  });

  const items = $derived([
    { value: "form", label: t("ui.details") },
    ...(row && tabs ? screen.children.map((c) => ({ value: c.id, label: screenLabel(c) })) : []),
    ...(row && tabs ? [{ value: "history", label: t("ui.history") }] : []),
  ]);
  const rootId = $derived(row && (tbl.root === tbl.name ? (row.id as number) : (row[tbl.child_fk!] as number)));
  const linked = (c: Screen) => Object.fromEntries(Object.entries(c.link ?? {}).map(([p, col]) => [p, row![col]]));
</script>

{#if id == null || row}
  <Tabs bind:value={tab} {items}>
    {#snippet panel(v)}
      {#if v === "form"}
        <Form {tbl} {row} {initial} file={{ view: tbl.name, params: {} }} {parent} {onsaved} />
      {:else if v === "history"}
        <History root={tbl.root} id={rootId!} />
      {:else}
        {@const child = screen.children.find((c) => c.id === v)!}
        {#key row!.id}
          {@const C = screenComponent(child)}
          <C screen={child} link={linked(child)} parent={row!} />
        {/key}
      {/if}
    {/snippet}
  </Tabs>
{/if}
