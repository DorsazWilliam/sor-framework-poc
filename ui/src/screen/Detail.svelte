<script lang="ts">
  import type { Row, Screen, Table } from "../api/contract";
  import Tabs from "../ds/Tabs.svelte";
  import Form from "./Form.svelte";
  import History from "./History.svelte";
  import ViewScreen from "./ViewScreen.svelte";
  import FileView from "./FileView.svelte";
  import { api } from "../api/client";
  import { watch } from "../app/live.svelte";
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

  async function load() {
    if (id == null || !list) return;
    row = (await api.view(tbl.name, { filter: [{ col: "id", op: "eq", value: id }] }))[0] ?? null;
  }

  $effect(() => {
    row = null;
    if (id == null) return;
    load();
    return watch(list?.roots ?? [tbl.root], load);
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
          {#if child.component === "pdf"}<FileView screen={child} link={linked(child)} />{:else}<ViewScreen screen={child} link={linked(child)} parent={row!} />{/if}
        {/key}
      {/if}
    {/snippet}
  </Tabs>
{/if}
